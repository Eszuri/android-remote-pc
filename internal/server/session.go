package server

import (
	"encoding/binary"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"arpc/internal/platform"
	"arpc/internal/protocol"
)

type ViewerSession struct {
	Stream   *protocol.SecureStream
	OnUnbind func()

	mu       sync.RWMutex
	rx, ry   float64
	rw, rh   float64
	maxWidth int
	jpegQ    int
	forceKF  bool
	closed   atomic.Bool
}

func NewViewerSession(stream *protocol.SecureStream) *ViewerSession {
	vs := &ViewerSession{
		Stream:   stream,
		rw:       1.0,
		rh:       1.0,
		maxWidth: 1280,
		jpegQ:    75,
		forceKF:  true,
	}
	go vs.videoLoop()
	return vs
}

func (vs *ViewerSession) Close() {
	if vs.closed.CompareAndSwap(false, true) {
		vs.Stream.Close()
	}
}

func (vs *ViewerSession) RequestKeyframe() {
	vs.mu.Lock()
	vs.forceKF = true
	vs.mu.Unlock()
}

func (vs *ViewerSession) ReadEventsFrom(ws *protocol.WSConn, epsFn func() (lan, wan []string)) {
	for !vs.closed.Load() {
		plain, err := vs.Stream.ReadFrom(ws)
		if err != nil {
			_ = ws.Close()
			if vs.Stream.IsCurrentWS(ws) {
				go func(expected *protocol.WSConn) {
					time.Sleep(12 * time.Second)
					if vs.Stream.IsCurrentWS(expected) {
						vs.Close()
					}
				}(ws)
			}
			return
		}
		if len(plain) == 0 {
			continue
		}
		switch plain[0] {
		case protocol.FrameInputMouse:
			flags, nx, ny, wheel, err := protocol.ParseMouseFrame(plain)
			if err == nil {
				platform.HandleMouse(flags, nx, ny, wheel)
			}
		case protocol.FrameInputKey:
			if len(plain) >= 4 {
				flags := plain[1]
				vk := binary.BigEndian.Uint16(plain[2:4])
				txt := string(plain[4:])
				platform.HandleKey(flags, vk, txt)
			}
		case protocol.FrameControl:
			var cmd protocol.ControlMsg
			if json.Unmarshal(plain[1:], &cmd) != nil {
				continue
			}
			switch cmd.Cmd {
			case "roi":
				vs.mu.Lock()
				vs.rx, vs.ry, vs.rw, vs.rh = cmd.ROIX, cmd.ROIY, cmd.ROIW, cmd.ROIH
				if cmd.MaxWidth > 0 {
					vs.maxWidth = cmd.MaxWidth
				}
				if cmd.PreferredJPEGQ > 0 {
					vs.jpegQ = cmd.PreferredJPEGQ
				}
				if cmd.ForceKeyframe {
					vs.forceKF = true
				}
				vs.mu.Unlock()
			case "unbind":
				if vs.OnUnbind != nil {
					go vs.OnUnbind()
				}
				return
			case "ping":
				pong := protocol.PackControlMsg(protocol.ControlMsg{Cmd: "pong", ClientSentMS: cmd.ClientSentMS})
				_ = vs.Stream.WritePacket(pong)
				if epsFn != nil {
					lan, wan := epsFn()
					tel := protocol.PackTelemetryMsg(protocol.TelemetryMsg{
						LANEndpoints: lan,
						WANEndpoints: wan,
					})
					_ = vs.Stream.WritePacket(tel)
				}
			}
		}
	}
}

func (vs *ViewerSession) videoLoop() {
	sendCh := make(chan []byte, 1)
	go func() {
		for pkt := range sendCh {
			if vs.closed.Load() {
				return
			}
			_ = vs.Stream.WritePacket(pkt)
		}
	}()
	defer close(sendCh)

	var prevHash uint64
	for !vs.closed.Load() {
		tStart := time.Now()
		mode := vs.Stream.Mode()
		targetInterval := 16 * time.Millisecond
		if mode != "LAN_DIRECT" {
			targetInterval = 33 * time.Millisecond
		}

		vs.mu.RLock()
		rx, ry, rw, rh := vs.rx, vs.ry, vs.rw, vs.rh
		mw, jq, fkf := vs.maxWidth, vs.jpegQ, vs.forceKF
		vs.mu.RUnlock()
		if fkf {
			vs.mu.Lock()
			vs.forceKF = false
			vs.mu.Unlock()
		}

		capRes, err := platform.CaptureROI(rx, ry, rw, rh, mw, jq, prevHash, fkf)
		if err == nil && capRes != nil {
			prevHash = capRes.Hash
			if !capRes.Unchanged && len(capRes.JPEG) > 0 {
				pkt := protocol.PackVideoFrame(capRes.JPEG)
				select {
				case sendCh <- pkt:
				default:
					select {
					case <-sendCh:
					default:
					}
					select {
					case sendCh <- pkt:
					default:
					}
				}
			}
		}

		if elapsed := time.Since(tStart); elapsed < targetInterval {
			time.Sleep(targetInterval - elapsed)
		}
	}
}
