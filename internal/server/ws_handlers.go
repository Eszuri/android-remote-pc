package server

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"arpc/internal/crypto"
	"arpc/internal/protocol"
)

func classifyConnMode(r *http.Request) string {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-Ip") != "" {
		return "WAN_DIRECT"
	}
	hostStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		hostStr = r.RemoteAddr
	}
	ip := net.ParseIP(hostStr)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return "LAN_DIRECT"
	}
	return "WAN_DIRECT"
}

func (s *Service) handlePairWS(w http.ResponseWriter, r *http.Request) {
	if s.Pairs.IsBound() {
		protocol.StealthDrop(w)
		return
	}
	ws, err := protocol.UpgradeWS(w, r)
	if err != nil {
		return
	}
	s.ServePairConn(ws)
}

func (s *Service) ServePairConn(ws *protocol.WSConn) {
	defer ws.Close()
	if s.Pairs.IsBound() {
		return
	}

	for {
		_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var env protocol.HandshakeEnvelope
		if json.Unmarshal(data, &env) != nil {
			return
		}
		switch env.Type {
		case "pair_lookup":
			sealed, ok := s.Pairs.LookupSealedDescriptor(env.LookupID)
			if !ok {
				rb, _ := json.Marshal(protocol.HandshakeEnvelope{Type: "err", Error: "qr_expired"})
				_ = ws.WriteText(string(rb))
				return
			}
			rb, _ := json.Marshal(protocol.HandshakeEnvelope{Type: "pair_desc", SealedDesc: sealed})
			_ = ws.WriteText(string(rb))
		case "pair_req":
			reply := s.Pairs.HandlePairReq(&env)
			rb, _ := json.Marshal(reply)
			_ = ws.WriteText(string(rb))
			time.Sleep(60 * time.Millisecond)
			return
		}
	}
}

func (s *Service) checkSessionAuthToken(tokHex string) (string, [32]byte) {
	tokBytes, err := hex.DecodeString(tokHex)
	if err != nil {
		return "", [32]byte{}
	}
	now := time.Now().Unix()
	if !s.Pairs.IsBound() {
		if ps, ok := s.Pairs.PrevPairSecret(); ok && crypto.VerifyBlindKnock(ps, "WS-AUTH", tokBytes, now) {
			return "prev", ps
		}
		return "", [32]byte{}
	}
	if crypto.VerifyBlindKnock(s.Pairs.PairSecret(), "WS-AUTH", tokBytes, now) {
		return "ok", [32]byte{}
	}
	return "", [32]byte{}
}

func writeSignedUnboundNotice(ws *protocol.WSConn, prevSec [32]byte) {
	var nonce [12]byte
	_, _ = rand.Read(nonce[:])
	mac := crypto.HMACBytes(prevSec[:], append([]byte("UNBOUND:"), nonce[:]...))
	rb, _ := json.Marshal(protocol.HandshakeEnvelope{
		Type:  "unbound",
		Nonce: hex.EncodeToString(nonce[:]),
		Proof: hex.EncodeToString(mac[:]),
	})
	_ = ws.WriteText(string(rb))
	time.Sleep(80 * time.Millisecond)
	_ = ws.Close()
}

func (s *Service) handleDirectSessionWS(w http.ResponseWriter, r *http.Request) {
	st, prevSec := s.checkSessionAuthToken(r.URL.Query().Get("t"))
	if st == "" {
		protocol.StealthDrop(w)
		return
	}
	ws, err := protocol.UpgradeWS(w, r)
	if err != nil {
		return
	}
	if st == "prev" {
		writeSignedUnboundNotice(ws, prevSec)
		return
	}
	s.serveSessionPipe(ws, classifyConnMode(r))
}

func (s *Service) ServeRTCChannel(label string, ws *protocol.WSConn) {
	path, query, _ := strings.Cut(label, "?")
	vals, _ := url.ParseQuery(query)
	switch path {
	case "pair":
		s.ServePairConn(ws)
	case "session":
		st, prevSec := s.checkSessionAuthToken(vals.Get("t"))
		if st == "prev" {
			writeSignedUnboundNotice(ws, prevSec)
			return
		}
		if st != "ok" {
			_ = ws.Close()
			return
		}
		s.serveSessionPipe(ws, "WAN_DIRECT")
	case "unbind":
		defer ws.Close()
		tb, err := hex.DecodeString(vals.Get("t"))
		if err == nil && s.Pairs.IsBound() && crypto.VerifyBlindKnock(s.Pairs.PairSecret(), "UNBIND", tb, time.Now().Unix()) {
			_ = s.PerformUnbind()
		}
	default:
		_ = ws.Close()
	}
}

func (s *Service) serveSessionPipe(ws *protocol.WSConn, mode string) {
	_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	var env protocol.HandshakeEnvelope
	if json.Unmarshal(data, &env) != nil {
		_ = ws.Close()
		return
	}

	switch env.Type {
	case "auth_hello":
		stream, reply, err := s.Pairs.VerifyAuthHello(&env, ws, mode)
		if err != nil {
			rb, _ := json.Marshal(protocol.HandshakeEnvelope{Type: "err", Error: err.Error()})
			_ = ws.WriteText(string(rb))
			_ = ws.Close()
			return
		}
		rb, _ := json.Marshal(reply)
		if err := ws.WriteText(string(rb)); err != nil {
			_ = ws.Close()
			return
		}

		s.mu.Lock()
		if s.session != nil {
			s.session.Close()
		}
		vs := NewViewerSession(stream)
		vs.OnUnbind = func() { _ = s.PerformUnbind() }
		s.session = vs
		s.mu.Unlock()

		vs.ReadEventsFrom(ws, s.Pairs.Endpoints)

	case "migrate_req":
		s.mu.Lock()
		vs := s.session
		s.mu.Unlock()

		if vs == nil || vs.closed.Load() || vs.Stream.SessionID != env.SessionID {
			rb, _ := json.Marshal(protocol.HandshakeEnvelope{Type: "err", Error: "no_active_session"})
			_ = ws.WriteText(string(rb))
			_ = ws.Close()
			return
		}
		nonce, _ := hex.DecodeString(env.Nonce)
		proof, _ := hex.DecodeString(env.Proof)
		msg := append([]byte("MIGRATE:"), append(vs.Stream.SessionAAD, nonce...)...)
		if len(nonce) != 12 || !crypto.VerifyHMAC(vs.Stream.KeyRecv[:], msg, proof) {
			_ = ws.Close()
			return
		}

		lanEps, wanEps := s.Pairs.Endpoints()
		ack, _ := json.Marshal(protocol.HandshakeEnvelope{
			Type:      "migrate_ok",
			SessionID: env.SessionID,
			LANEps:    lanEps,
			WANEps:    wanEps,
			ConnMode:  mode,
		})
		oldWS := vs.Stream.SwapTransport(ws, mode, string(ack))
		vs.RequestKeyframe()
		if oldWS != nil && oldWS != ws {
			_ = oldWS.Close()
		}
		vs.ReadEventsFrom(ws, s.Pairs.Endpoints)

	default:
		_ = ws.Close()
	}
}

func (s *Service) ServeUDPLANDiscovery(conn *net.UDPConn) {
	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 49 || binary.BigEndian.Uint32(buf[0:4]) != protocol.UDPMagic || buf[4] != protocol.UDPBlindKnock {
			continue
		}
		var targetSec [32]byte
		if s.Pairs.IsBound() {
			targetSec = s.Pairs.PairSecret()
		} else if prevSec, ok := s.Pairs.PrevPairSecret(); ok {
			targetSec = prevSec
		} else {
			continue
		}
		if !crypto.VerifyBlindKnock(targetSec, "UDP-LAN", buf[5:49], time.Now().Unix()) {
			continue
		}
		lanEps, wanEps := s.Pairs.Endpoints()
		jb, _ := json.Marshal(map[string]any{"lan": lanEps, "wan": wanEps})
		var nonce [12]byte
		_, _ = rand.Read(nonce[:])
		sealed := crypto.Seal(targetSec, nonce, jb, nil)
		resp := make([]byte, 5+12+len(sealed))
		binary.BigEndian.PutUint32(resp[0:4], protocol.UDPMagic)
		resp[4] = protocol.UDPKnockReply
		copy(resp[5:17], nonce[:])
		copy(resp[17:], sealed)
		_, _ = conn.WriteToUDP(resp, addr)
	}
}
