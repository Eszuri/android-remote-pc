package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"arpc/internal/protocol"
)

var defaultRTCTrackers = []string{
	"wss://tracker.openwebtorrent.com",
	"wss://tracker.novage.com.ua",
}

type trackerEnvelope struct {
	Action   string          `json:"action"`
	InfoHash string          `json:"info_hash"`
	PeerID   string          `json:"peer_id"`
	ToPeerID string          `json:"to_peer_id,omitempty"`
	OfferID  string          `json:"offer_id,omitempty"`
	NumWant  int             `json:"numwant,omitempty"`
	Left     int             `json:"left,omitempty"`
	Offer    *sdpDescPayload `json:"offer,omitempty"`
	Answer   *sdpDescPayload `json:"answer,omitempty"`
}

type sdpDescPayload struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type WebRTCHost struct {
	Service  *Service
	OnOnline func(online bool)

	peerID    string
	wakeCh    chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	liveConns atomic.Int32

	seenMu     sync.Mutex
	seenOffers map[string]time.Time
}

func NewWebRTCHost(svc *Service) *WebRTCHost {
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	return &WebRTCHost{
		Service:    svc,
		peerID:     "-AR1000-" + hex.EncodeToString(rnd[:]),
		wakeCh:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
		seenOffers: make(map[string]time.Time),
	}
}

func (h *WebRTCHost) Start() {
	for _, trURL := range defaultRTCTrackers {
		go h.trackerLoop(trURL)
	}
}

func (h *WebRTCHost) NotifyStateChanged() {
	select {
	case h.wakeCh <- struct{}{}:
	default:
	}
}

func (h *WebRTCHost) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
}

func (h *WebRTCHost) markOfferSeen(peerID, offerID string) bool {
	if peerID == "" || offerID == "" {
		return false
	}
	key := peerID + ":" + offerID
	now := time.Now()
	h.seenMu.Lock()
	defer h.seenMu.Unlock()
	for k, exp := range h.seenOffers {
		if now.After(exp) {
			delete(h.seenOffers, k)
		}
	}
	if _, exists := h.seenOffers[key]; exists {
		return false
	}
	h.seenOffers[key] = now.Add(45 * time.Second)
	return true
}

func (h *WebRTCHost) trackerLoop(trURL string) {
	backoff := 2 * time.Second
	for {
		select {
		case <-h.stopCh:
			return
		default:
		}

		ws, err := protocol.DialWS(trURL, map[string]string{"User-Agent": "ARPC"}, 7*time.Second)
		if err != nil {
			select {
			case <-h.stopCh:
				return
			case <-time.After(backoff):
			}
			if backoff < 15*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		if h.liveConns.Add(1) == 1 && h.OnOnline != nil {
			h.OnOnline(true)
		}

		h.serveTrackerConn(ws)

		if h.liveConns.Add(-1) == 0 && h.OnOnline != nil {
			h.OnOnline(false)
		}
		select {
		case <-h.stopCh:
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (h *WebRTCHost) serveTrackerConn(ws *protocol.WSConn) {
	connDone := make(chan struct{})
	defer func() {
		close(connDone)
		_ = ws.Close()
	}()

	announceRooms := func() error {
		for _, room := range h.Service.Pairs.SignalingRooms() {
			jb, _ := json.Marshal(trackerEnvelope{
				Action:   "announce",
				InfoHash: room,
				PeerID:   h.peerID,
				NumWant:  5,
				Left:     1,
			})
			if err := ws.WriteText(string(jb)); err != nil {
				return err
			}
		}
		return nil
	}

	if err := announceRooms(); err != nil {
		return
	}

	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-connDone:
				return
			case <-h.stopCh:
				_ = ws.Close()
				return
			case <-h.wakeCh:
				if announceRooms() != nil {
					_ = ws.Close()
					return
				}
			case <-t.C:
				if announceRooms() != nil {
					_ = ws.Close()
					return
				}
			}
		}
	}()

	for {
		_ = ws.SetReadDeadline(time.Now().Add(65 * time.Second))
		op, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if op != protocol.OpText {
			continue
		}
		var env trackerEnvelope
		if json.Unmarshal(raw, &env) != nil || env.Offer == nil || env.Offer.SDP == "" {
			continue
		}
		if !h.markOfferSeen(env.PeerID, env.OfferID) {
			continue
		}

		go func(req trackerEnvelope) {
			ansSDP, err := h.Service.AcceptOfferSDP(req.Offer.SDP)
			if err != nil {
				return
			}
			resp := trackerEnvelope{
				Action:   "announce",
				InfoHash: req.InfoHash,
				PeerID:   h.peerID,
				ToPeerID: req.PeerID,
				OfferID:  req.OfferID,
				Answer: &sdpDescPayload{
					Type: "answer",
					SDP:  ansSDP,
				},
			}
			jb, _ := json.Marshal(resp)
			_ = ws.WriteText(string(jb))
		}(env)
	}
}
