package server

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"arpc/internal/crypto"
	"arpc/internal/platform"
	"arpc/internal/protocol"
)

type persistedConfig struct {
	PCPriv         string `json:"pc_priv"`
	PCPub          string `json:"pc_pub"`
	Bound          bool   `json:"bound"`
	PhonePub       string `json:"phone_pub,omitempty"`
	PairSecret     string `json:"pair_secret,omitempty"`
	PrevPairSecret string `json:"prev_pair_secret,omitempty"`
}

type PairManager struct {
	Hostname  string
	StatePath string

	mu             sync.RWMutex
	pcPriv         [32]byte
	pcPub          [32]byte
	bound          bool
	phonePub       [32]byte
	pairSecret     [32]byte
	prevPairSecret [32]byte
	hasPrevSecret  bool

	currOTP       []byte
	prevOTP       []byte
	otpRotated    time.Time
	lanEps        []string
	wanEps        []string
	OnStateChange func()
}

func NewPairManager(hostname, statePath string) (*PairManager, error) {
	pm := &PairManager{
		Hostname:  hostname,
		StatePath: statePath,
	}
	if raw, err := platform.LoadProtectedFile(statePath); err == nil && len(raw) > 0 {
		var cfg persistedConfig
		if json.Unmarshal(raw, &cfg) == nil {
			privB, _ := hex.DecodeString(cfg.PCPriv)
			pubB, _ := hex.DecodeString(cfg.PCPub)
			if len(privB) == 32 && len(pubB) == 32 {
				copy(pm.pcPriv[:], privB)
				copy(pm.pcPub[:], pubB)
				pm.bound = cfg.Bound
				if pm.bound {
					pp, _ := hex.DecodeString(cfg.PhonePub)
					ps, _ := hex.DecodeString(cfg.PairSecret)
					copy(pm.phonePub[:], pp)
					copy(pm.pairSecret[:], ps)
				} else if prevB, err := hex.DecodeString(cfg.PrevPairSecret); err == nil && len(prevB) == 32 {
					copy(pm.prevPairSecret[:], prevB)
					pm.hasPrevSecret = true
				}
				pm.rotateOTPIfNeededLocked()
				return pm, nil
			}
		}
	}
	priv, pub, err := crypto.GenerateX25519()
	if err != nil {
		return nil, err
	}
	pm.pcPriv = priv
	pm.pcPub = pub
	pm.rotateOTPIfNeededLocked()
	_ = pm.saveLocked()
	return pm, nil
}

func (pm *PairManager) saveLocked() error {
	cfg := persistedConfig{
		PCPriv: hex.EncodeToString(pm.pcPriv[:]),
		PCPub:  hex.EncodeToString(pm.pcPub[:]),
		Bound:  pm.bound,
	}
	if pm.bound {
		cfg.PhonePub = hex.EncodeToString(pm.phonePub[:])
		cfg.PairSecret = hex.EncodeToString(pm.pairSecret[:])
	} else if pm.hasPrevSecret {
		cfg.PrevPairSecret = hex.EncodeToString(pm.prevPairSecret[:])
	}
	jb, _ := json.Marshal(cfg)
	return platform.SaveProtectedFile(pm.StatePath, jb)
}

func (pm *PairManager) ResetUnbind() error {
	pm.mu.Lock()
	if pm.bound {
		pm.prevPairSecret = pm.pairSecret
		pm.hasPrevSecret = true
	}
	pm.bound = false
	pm.phonePub = [32]byte{}
	pm.pairSecret = [32]byte{}
	pm.currOTP = nil
	pm.rotateOTPIfNeededLocked()
	err := pm.saveLocked()
	sc := pm.OnStateChange
	pm.mu.Unlock()
	if sc != nil {
		go sc()
	}
	return err
}

func rtcRoomHash(tag string, secret []byte) string {
	h := sha256.Sum256(append([]byte(tag), secret...))
	return hex.EncodeToString(h[:10])
}

func (pm *PairManager) PairRoomTag() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return "rtc:" + rtcRoomHash("arpc-rtc-pair-v1:", pm.pcPub[:])
}

func (pm *PairManager) SignalingRooms() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bound {
		return []string{rtcRoomHash("arpc-rtc-bound-v1:", pm.pairSecret[:])}
	}
	rooms := []string{rtcRoomHash("arpc-rtc-pair-v1:", pm.pcPub[:])}
	if pm.hasPrevSecret {
		rooms = append(rooms, rtcRoomHash("arpc-rtc-bound-v1:", pm.prevPairSecret[:]))
	}
	return rooms
}

func (pm *PairManager) PrevPairSecret() ([32]byte, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.prevPairSecret, pm.hasPrevSecret
}

func (pm *PairManager) IsBound() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.bound
}

func (pm *PairManager) PairSecret() [32]byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pairSecret
}

func (pm *PairManager) SetEndpoints(lan, wan []string) {
	pm.mu.Lock()
	pm.lanEps = append([]string(nil), lan...)
	pm.wanEps = append([]string(nil), wan...)
	pm.mu.Unlock()
}

func (pm *PairManager) Endpoints() (lan, wan []string) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return append([]string(nil), pm.lanEps...), append([]string(nil), pm.wanEps...)
}

func (pm *PairManager) rotateOTPIfNeededLocked() {
	if pm.bound {
		return
	}
	if len(pm.currOTP) == 0 || time.Since(pm.otpRotated) >= 55*time.Second {
		pm.prevOTP = pm.currOTP
		otp := make([]byte, 16)
		_, _ = rand.Read(otp)
		pm.currOTP = otp
		pm.otpRotated = time.Now()
	}
}

func (pm *PairManager) sealDescriptorLocked(otp []byte) string {
	desc := protocol.PairDescriptor{
		PCPub:    hex.EncodeToString(pm.pcPub[:]),
		LANEps:   pm.lanEps,
		WANEps:   pm.wanEps,
		Hostname: pm.Hostname,
	}
	jb, _ := json.Marshal(desc)
	dkSlice := crypto.HKDFSHA256(otp, nil, []byte("arpc-desc-v1"), 32)
	var dk [32]byte
	copy(dk[:], dkSlice)
	var nonce [12]byte
	_, _ = rand.Read(nonce[:])
	ct := crypto.Seal(dk, nonce, jb, nil)
	return hex.EncodeToString(append(nonce[:], ct...))
}

func (pm *PairManager) CurrentOneScanURL(baseHTTP string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.bound {
		return "", errors.New("already bound 1-to-1")
	}
	pm.rotateOTPIfNeededLocked()
	otp := pm.currOTP

	lanCSV := url.QueryEscape(strings.Join(pm.lanEps, ","))
	qrURL := strings.TrimRight(baseHTTP, "/") + "/#p=" + hex.EncodeToString(otp) + "&l=" + lanCSV
	if len(pm.wanEps) > 0 {
		qrURL += "&w=" + url.QueryEscape(strings.Join(pm.wanEps, ","))
	}
	return qrURL, nil
}

func (pm *PairManager) LookupSealedDescriptor(lid string) (string, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bound || len(pm.currOTP) == 0 {
		return "", false
	}
	for _, otp := range [][]byte{pm.currOTP, pm.prevOTP} {
		if len(otp) == 0 || crypto.PairLookupID(otp) != lid {
			continue
		}
		return pm.sealDescriptorLocked(otp), true
	}
	return "", false
}

func (pm *PairManager) HandlePairReq(env *protocol.HandshakeEnvelope) *protocol.HandshakeEnvelope {
	pm.mu.Lock()
	if pm.bound {
		pm.mu.Unlock()
		return &protocol.HandshakeEnvelope{Type: "err", Error: "single_slot_locked"}
	}
	phonePubSlice, err1 := hex.DecodeString(env.PhonePub)
	nonceSlice, err2 := hex.DecodeString(env.Nonce)
	proofSlice, err3 := hex.DecodeString(env.Proof)
	if err1 != nil || err2 != nil || err3 != nil || len(phonePubSlice) != 32 || len(nonceSlice) != 12 {
		pm.mu.Unlock()
		return &protocol.HandshakeEnvelope{Type: "err", Error: "bad_pair_fields"}
	}
	var phonePub [32]byte
	var nonce [12]byte
	copy(phonePub[:], phonePubSlice)
	copy(nonce[:], nonceSlice)

	shared, err := crypto.SharedX25519(pm.pcPriv, phonePub)
	if err != nil {
		pm.mu.Unlock()
		return &protocol.HandshakeEnvelope{Type: "err", Error: "ecdh_error"}
	}

	var matchedOTP []byte
	var matchedPTK [32]byte
	expPlain := append([]byte("PAIR:"), append(phonePub[:], pm.pcPub[:]...)...)
	for _, otp := range [][]byte{pm.currOTP, pm.prevOTP} {
		if len(otp) == 0 {
			continue
		}
		ptkSlice := crypto.HKDFSHA256(shared[:], otp, []byte("arpc-pair-v1"), 32)
		var ptk [32]byte
		copy(ptk[:], ptkSlice)
		plain, err := crypto.Open(ptk, nonce, proofSlice, nil)
		if err == nil && bytes.Equal(plain, expPlain) {
			matchedOTP = otp
			matchedPTK = ptk
			break
		}
	}
	if matchedOTP == nil {
		pm.mu.Unlock()
		return &protocol.HandshakeEnvelope{Type: "err", Error: "invalid_qr_token"}
	}

	psSlice := crypto.HKDFSHA256(shared[:], matchedOTP, []byte("arpc-master-secret-v1"), 32)
	copy(pm.pairSecret[:], psSlice)
	pm.prevPairSecret = [32]byte{}
	pm.hasPrevSecret = false
	pm.phonePub = phonePub
	pm.bound = true
	pm.currOTP = nil
	pm.prevOTP = nil
	_ = pm.saveLocked()
	lanEps := append([]string(nil), pm.lanEps...)
	wanEps := append([]string(nil), pm.wanEps...)
	sc := pm.OnStateChange
	pm.mu.Unlock()

	if sc != nil {
		go sc()
	}

	var ackNonce [12]byte
	_, _ = rand.Read(ackNonce[:])
	ackProof := crypto.Seal(matchedPTK, ackNonce, []byte("PAIRED_OK"), nil)
	return &protocol.HandshakeEnvelope{
		Type:   "pair_ack",
		Nonce:  hex.EncodeToString(ackNonce[:]),
		Proof:  hex.EncodeToString(ackProof),
		LANEps: lanEps,
		WANEps: wanEps,
	}
}

func (pm *PairManager) VerifyAuthHello(env *protocol.HandshakeEnvelope, ws *protocol.WSConn, mode string) (*protocol.SecureStream, *protocol.HandshakeEnvelope, error) {
	pm.mu.RLock()
	bound := pm.bound
	expPhone := pm.phonePub
	pairSec := pm.pairSecret
	lanEps := append([]string(nil), pm.lanEps...)
	wanEps := append([]string(nil), pm.wanEps...)
	pm.mu.RUnlock()

	if !bound {
		return nil, nil, errors.New("unbound")
	}
	gotPhone, _ := hex.DecodeString(env.PhonePub)
	ephPhoneSlice, _ := hex.DecodeString(env.EphPub)
	proof, _ := hex.DecodeString(env.Proof)
	if len(gotPhone) != 32 || len(ephPhoneSlice) != 32 || !bytes.Equal(gotPhone, expPhone[:]) {
		return nil, nil, errors.New("unauthorized_phone")
	}
	msg := append([]byte("HELLO:"), ephPhoneSlice...)
	if !crypto.VerifyHMAC(pairSec[:], msg, proof) {
		return nil, nil, errors.New("bad_hello_mac")
	}

	var ephPhone [32]byte
	copy(ephPhone[:], ephPhoneSlice)
	pcEphPriv, pcEphPub, err := crypto.GenerateX25519()
	if err != nil {
		return nil, nil, err
	}
	shared, err := crypto.SharedX25519(pcEphPriv, ephPhone)
	if err != nil {
		return nil, nil, err
	}
	var sidBytes [16]byte
	_, _ = rand.Read(sidBytes[:])
	sidHex := hex.EncodeToString(sidBytes[:])

	info := append([]byte("arpc-session-v1:"), sidBytes[:]...)
	keys := crypto.HKDFSHA256(shared[:], pairSec[:], info, 64)
	var kC2S, kS2C [32]byte
	copy(kC2S[:], keys[0:32])
	copy(kS2C[:], keys[32:64])

	stream := protocol.NewSecureStream(sidHex, sidBytes[:], kC2S, kS2C, 0x53324300, ws, mode)
	reply := &protocol.HandshakeEnvelope{
		Type:      "auth_ok",
		EphPub:    hex.EncodeToString(pcEphPub[:]),
		SessionID: sidHex,
		LANEps:    lanEps,
		WANEps:    wanEps,
		ConnMode:  mode,
	}
	return stream, reply, nil
}
