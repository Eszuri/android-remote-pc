package crypto

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

var ErrAuthFailed = errors.New("authentication failed")

func GenerateX25519() (privBytes [32]byte, pubBytes [32]byte, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return privBytes, pubBytes, err
	}
	copy(privBytes[:], priv.Bytes())
	copy(pubBytes[:], priv.PublicKey().Bytes())
	return privBytes, pubBytes, nil
}

func SharedX25519(privBytes, peerPubBytes [32]byte) ([32]byte, error) {
	var out [32]byte
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(privBytes[:])
	if err != nil {
		return out, err
	}
	pub, err := curve.NewPublicKey(peerPubBytes[:])
	if err != nil {
		return out, err
	}
	sec, err := priv.ECDH(pub)
	if err != nil {
		return out, err
	}
	copy(out[:], sec)
	return out, nil
}

func HKDFSHA256(ikm, salt, info []byte, length int) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	ext := hmac.New(sha256.New, salt)
	ext.Write(ikm)
	prk := ext.Sum(nil)

	out := make([]byte, 0, length)
	var prev []byte
	var counter byte = 1
	for len(out) < length {
		mac := hmac.New(sha256.New, prk)
		mac.Write(prev)
		mac.Write(info)
		mac.Write([]byte{counter})
		prev = mac.Sum(nil)
		out = append(out, prev...)
		counter++
	}
	return out[:length]
}

func PairLookupID(otp []byte) string {
	h := sha256.Sum256(append([]byte("arpc-pair-lookup-v1:"), otp...))
	return hex.EncodeToString(h[:8])
}

func VerifyBlindKnock(pairSecret [32]byte, purpose string, token []byte, unixSec int64) bool {
	if len(token) != 44 {
		return false
	}
	nonce := token[:12]
	gotMAC := token[12:]
	curWin := uint64(unixSec / 15)
	for _, w := range []uint64{curWin, curWin - 1, curWin + 1} {
		exp := computeKnockMAC(pairSecret, purpose, w, nonce)
		if subtle.ConstantTimeCompare(gotMAC, exp[:]) == 1 {
			return true
		}
	}
	return false
}

func computeKnockMAC(secret [32]byte, purpose string, win uint64, nonce []byte) [32]byte {
	var winBuf [8]byte
	binary.BigEndian.PutUint64(winBuf[:], win)
	m := hmac.New(sha256.New, secret[:])
	m.Write([]byte(purpose))
	m.Write(winBuf[:])
	m.Write(nonce)
	var out [32]byte
	copy(out[:], m.Sum(nil))
	return out
}

func HMACBytes(key, data []byte) [32]byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	var out [32]byte
	copy(out[:], m.Sum(nil))
	return out
}

func VerifyHMAC(key, data, mac []byte) bool {
	if len(mac) != 32 {
		return false
	}
	exp := HMACBytes(key, data)
	return subtle.ConstantTimeCompare(exp[:], mac) == 1
}
