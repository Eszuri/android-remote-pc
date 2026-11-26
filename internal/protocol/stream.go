package protocol

import (
	"sync"
	"sync/atomic"

	"arpc/internal/crypto"
)

type SecureStream struct {
	SessionID  string
	SessionAAD []byte
	KeyRecv    [32]byte
	KeySend    [32]byte
	SendPrefix uint32

	mu      sync.RWMutex
	ws      *WSConn
	mode    string
	sendSeq atomic.Uint64
}

func NewSecureStream(idHex string, idBytes []byte, keyRecv, keySend [32]byte, sendPrefix uint32, ws *WSConn, mode string) *SecureStream {
	s := &SecureStream{
		SessionID:  idHex,
		SessionAAD: idBytes,
		KeyRecv:    keyRecv,
		KeySend:    keySend,
		SendPrefix: sendPrefix,
		ws:         ws,
		mode:       mode,
	}
	s.sendSeq.Store(1)
	return s
}

func (s *SecureStream) Mode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *SecureStream) SwapTransport(newWS *WSConn, newMode string, ackJSON string) *WSConn {
	s.mu.Lock()
	old := s.ws
	_ = newWS.WriteText(ackJSON)
	s.ws = newWS
	s.mode = newMode
	s.mu.Unlock()
	return old
}

func (s *SecureStream) WritePacket(plain []byte) error {
	seq := s.sendSeq.Add(1) - 1
	nonce := crypto.CounterNonce(s.SendPrefix, seq)
	ct := crypto.Seal(s.KeySend, nonce, plain, s.SessionAAD)
	pkt := make([]byte, 12+len(ct))
	copy(pkt[:12], nonce[:])
	copy(pkt[12:], ct)

	s.mu.RLock()
	ws := s.ws
	s.mu.RUnlock()
	if ws == nil {
		return nil
	}
	return ws.WriteBinary(pkt)
}

func (s *SecureStream) ReadFrom(ws *WSConn) ([]byte, error) {
	_, raw, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	if len(raw) < 28 {
		return nil, crypto.ErrAuthFailed
	}
	var nonce [12]byte
	copy(nonce[:], raw[:12])
	return crypto.Open(s.KeyRecv, nonce, raw[12:], s.SessionAAD)
}

func (s *SecureStream) IsCurrentWS(ws *WSConn) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ws == ws
}

func (s *SecureStream) IsLive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ws != nil && !s.ws.closed
}

func (s *SecureStream) Close() {
	s.mu.Lock()
	ws := s.ws
	s.ws = nil
	s.mu.Unlock()
	if ws != nil {
		_ = ws.Close()
	}
}
