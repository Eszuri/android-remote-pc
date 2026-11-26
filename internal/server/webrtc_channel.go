package server

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/webrtc/v4"

	"arpc/internal/protocol"
)

const (
	dcChunkSingle = 0x00
	dcChunkPart   = 0x01
	dcChunkLast   = 0x02
	dcMaxChunk    = 48 * 1024
)

var defaultICEServers = []webrtc.ICEServer{
	{URLs: []string{
		"stun:stun.l.google.com:19302",
		"stun:stun1.l.google.com:19302",
		"stun:stun.cloudflare.com:3478",
	}},
}

type dcFrame struct {
	op   byte
	data []byte
}

type dcMsgConn struct {
	pc        *webrtc.PeerConnection
	dc        *webrtc.DataChannel
	recvCh    chan dcFrame
	bufLowCh  chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once

	dlMu     sync.Mutex
	deadline time.Time
}

func newDCMsgConn(pc *webrtc.PeerConnection, dc *webrtc.DataChannel) *dcMsgConn {
	c := &dcMsgConn{
		pc:       pc,
		dc:       dc,
		recvCh:   make(chan dcFrame, 128),
		bufLowCh: make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
	}
	dc.SetBufferedAmountLowThreshold(256 * 1024)
	dc.OnBufferedAmountLow(func() {
		select {
		case c.bufLowCh <- struct{}{}:
		default:
		}
	})

	var reasm []byte
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			cp := append([]byte(nil), msg.Data...)
			select {
			case c.recvCh <- dcFrame{op: protocol.OpText, data: cp}:
			case <-c.closeCh:
			}
			return
		}
		if len(msg.Data) == 0 {
			return
		}
		tag := msg.Data[0]
		body := msg.Data[1:]
		switch tag {
		case dcChunkSingle:
			cp := append([]byte(nil), body...)
			select {
			case c.recvCh <- dcFrame{op: protocol.OpBinary, data: cp}:
			case <-c.closeCh:
			}
		case dcChunkPart:
			if len(reasm)+len(body) <= 8*1024*1024 {
				reasm = append(reasm, body...)
			} else {
				reasm = nil
			}
		case dcChunkLast:
			full := make([]byte, len(reasm)+len(body))
			copy(full, reasm)
			copy(full[len(reasm):], body)
			reasm = nil
			select {
			case c.recvCh <- dcFrame{op: protocol.OpBinary, data: full}:
			case <-c.closeCh:
			}
		}
	})
	return c
}

func (c *dcMsgConn) ReadMessage() (byte, []byte, error) {
	c.dlMu.Lock()
	dl := c.deadline
	c.dlMu.Unlock()

	if !dl.IsZero() {
		wait := time.Until(dl)
		if wait <= 0 {
			return 0, nil, context.DeadlineExceeded
		}
		tmr := time.NewTimer(wait)
		defer tmr.Stop()
		select {
		case f := <-c.recvCh:
			return f.op, f.data, nil
		case <-c.closeCh:
			return 0, nil, io.EOF
		case <-tmr.C:
			return 0, nil, context.DeadlineExceeded
		}
	}

	select {
	case f := <-c.recvCh:
		return f.op, f.data, nil
	case <-c.closeCh:
		return 0, nil, io.EOF
	}
}

func (c *dcMsgConn) waitBackpressure() error {
	if c.dc.BufferedAmount() <= 512*1024 {
		return nil
	}
	tmr := time.NewTimer(120 * time.Millisecond)
	defer tmr.Stop()
	select {
	case <-c.bufLowCh:
		return nil
	case <-tmr.C:
		return nil
	case <-c.closeCh:
		return net.ErrClosed
	}
}

func (c *dcMsgConn) WriteText(text string) error {
	select {
	case <-c.closeCh:
		return net.ErrClosed
	default:
	}
	return c.dc.SendText(text)
}

func (c *dcMsgConn) WriteBinary(data []byte) error {
	if err := c.waitBackpressure(); err != nil {
		return err
	}
	if len(data) <= dcMaxChunk {
		pkt := make([]byte, 1+len(data))
		pkt[0] = dcChunkSingle
		copy(pkt[1:], data)
		return c.dc.Send(pkt)
	}
	for off := 0; off < len(data); off += dcMaxChunk {
		end := off + dcMaxChunk
		tag := byte(dcChunkPart)
		if end >= len(data) {
			end = len(data)
			tag = dcChunkLast
		}
		chunk := data[off:end]
		pkt := make([]byte, 1+len(chunk))
		pkt[0] = tag
		copy(pkt[1:], chunk)
		if err := c.dc.Send(pkt); err != nil {
			return err
		}
	}
	return nil
}

func (c *dcMsgConn) SetReadDeadline(t time.Time) error {
	c.dlMu.Lock()
	c.deadline = t
	c.dlMu.Unlock()
	return nil
}

func (c *dcMsgConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		_ = c.dc.Close()
		_ = c.pc.Close()
	})
	return nil
}

func newWebRTCAPI() *webrtc.API {
	var se webrtc.SettingEngine
	if nw, err := stdnet.NewNet(); err == nil {
		se.SetNet(nw)
	}
	se.SetSCTPMaxReceiveBufferSize(1024 * 1024)
	return webrtc.NewAPI(webrtc.WithSettingEngine(se))
}

func (s *Service) AcceptOfferSDP(offerSDP string) (string, error) {
	api := newWebRTCAPI()
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: defaultICEServers})
	if err != nil {
		return "", err
	}

	var opened atomic.Bool
	time.AfterFunc(18*time.Second, func() {
		if !opened.Load() {
			_ = pc.Close()
		}
	})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		mc := newDCMsgConn(pc, dc)
		ws := protocol.NewWSConnFromMsg(mc)

		dc.OnOpen(func() {
			opened.Store(true)
			go s.ServeRTCChannel(label, ws)
		})
		dc.OnClose(func() {
			_ = ws.Close()
		})
	})

	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			_ = pc.Close()
		} else if st == webrtc.PeerConnectionStateDisconnected {
			time.AfterFunc(6*time.Second, func() {
				if pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					_ = pc.Close()
				}
			})
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		_ = pc.Close()
		return "", err
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", err
	}

	select {
	case <-gatherDone:
	case <-time.After(2200 * time.Millisecond):
	}
	ld := pc.LocalDescription()
	if ld == nil {
		_ = pc.Close()
		return "", errors.New("empty local description")
	}
	return ld.SDP, nil
}
