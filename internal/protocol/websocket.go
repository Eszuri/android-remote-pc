package protocol

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	OpText   = 0x1
	OpBinary = 0x2
	OpClose  = 0x8
	OpPing   = 0x9
	OpPong   = 0xA
)

type MsgConn interface {
	ReadMessage() (opcode byte, payload []byte, err error)
	WriteBinary(data []byte) error
	WriteText(text string) error
	SetReadDeadline(t time.Time) error
	Close() error
}

type WSConn struct {
	conn     net.Conn
	br       *bufio.Reader
	alt      MsgConn
	isClient bool
	writeMu  sync.Mutex
	closed   bool
}

func NewWSConnFromMsg(mc MsgConn) *WSConn {
	return &WSConn{alt: mc}
}

func StealthDrop(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if c, _, err := hj.Hijack(); err == nil {
			_ = c.Close()
			return
		}
	}
}

func UpgradeWS(w http.ResponseWriter, r *http.Request) (*WSConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("hijacking unsupported")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetWriteBuffer(512 * 1024)
	}

	h := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h[:])

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &WSConn{conn: conn, br: brw.Reader, isClient: false}, nil
}

func DialWS(rawURL string, headers map[string]string, timeout time.Duration) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	var keyBytes [16]byte
	_, _ = rand.Read(keyBytes[:])
	wsKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	reqPath := u.RequestURI()
	if reqPath == "" {
		reqPath = "/"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n", reqPath, u.Host, wsKey)
	for k, v := range headers {
		fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
	}
	sb.WriteString("\r\n")

	if _, err := conn.Write([]byte(sb.String())); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 65536)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ws handshake rejected: %s", strings.TrimSpace(statusLine))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return &WSConn{conn: conn, br: br, isClient: true}, nil
}

func (w *WSConn) SetReadDeadline(t time.Time) error {
	if w.alt != nil {
		return w.alt.SetReadDeadline(t)
	}
	return w.conn.SetReadDeadline(t)
}

func (w *WSConn) ReadMessage() (opcode byte, payload []byte, err error) {
	if w.alt != nil {
		op, b, rerr := w.alt.ReadMessage()
		if rerr != nil {
			_ = w.Close()
		}
		return op, b, rerr
	}
	for {
		var hdr [2]byte
		if _, err = io.ReadFull(w.br, hdr[:]); err != nil {
			return 0, nil, err
		}
		op := hdr[0] & 0x0F
		masked := (hdr[1] & 0x80) != 0
		length := uint64(hdr[1] & 0x7F)

		switch length {
		case 126:
			var ext [2]byte
			if _, err = io.ReadFull(w.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err = io.ReadFull(w.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		if length > 16*1024*1024 {
			return 0, nil, errors.New("ws frame exceeds 16MB limit")
		}
		var maskKey [4]byte
		if masked {
			if _, err = io.ReadFull(w.br, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}
		data := make([]byte, length)
		if length > 0 {
			if _, err = io.ReadFull(w.br, data); err != nil {
				return 0, nil, err
			}
		}
		if masked {
			for i := range data {
				data[i] ^= maskKey[i&3]
			}
		}

		switch op {
		case OpPing:
			_ = w.WriteFrame(OpPong, data)
			continue
		case OpPong:
			continue
		case OpClose:
			_ = w.Close()
			return OpClose, nil, io.EOF
		default:
			return op, data, nil
		}
	}
}

func (w *WSConn) WriteBinary(data []byte) error {
	return w.WriteFrame(OpBinary, data)
}

func (w *WSConn) WriteText(text string) error {
	return w.WriteFrame(OpText, []byte(text))
}

func (w *WSConn) WriteFrame(opcode byte, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.closed {
		return net.ErrClosed
	}
	if w.alt != nil {
		if opcode == OpText {
			return w.alt.WriteText(string(data))
		}
		return w.alt.WriteBinary(data)
	}

	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|(opcode&0x0F))
	maskBit := byte(0)
	if w.isClient {
		maskBit = 0x80
	}
	n := len(data)
	switch {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n <= 65535:
		hdr = append(hdr, maskBit|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|127,
			byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32),
			byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}

	_ = w.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	if !w.isClient {
		if _, err := w.conn.Write(hdr); err != nil {
			return err
		}
		if n > 0 {
			_, err := w.conn.Write(data)
			return err
		}
		return nil
	}

	var maskKey [4]byte
	_, _ = rand.Read(maskKey[:])
	hdr = append(hdr, maskKey[:]...)
	masked := make([]byte, len(hdr)+n)
	copy(masked, hdr)
	for i := 0; i < n; i++ {
		masked[len(hdr)+i] = data[i] ^ maskKey[i&3]
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *WSConn) Close() error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.alt != nil {
		return w.alt.Close()
	}
	return w.conn.Close()
}
