package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

const UDPMagic uint32 = 0x4F504331

const (
	UDPBlindKnock byte = 0x01
	UDPKnockReply byte = 0x02
)

const (
	FrameInputMouse byte = 0x01
	FrameInputKey   byte = 0x02
	FrameControl    byte = 0x03
	FrameVideo      byte = 0x10
	FrameTelemetry  byte = 0x11
)

const (
	MouseMove      byte = 1 << 0
	MouseLeftDown  byte = 1 << 1
	MouseLeftUp    byte = 1 << 2
	MouseRightDown byte = 1 << 3
	MouseRightUp   byte = 1 << 4
	MouseWheel     byte = 1 << 5
	MouseMidClick  byte = 1 << 6
)

const (
	KeyDown    byte = 1 << 0
	KeyUp      byte = 1 << 1
	KeyUnicode byte = 1 << 2
	KeyCombo   byte = 1 << 3
)

type HandshakeEnvelope struct {
	Type       string   `json:"type"`
	LookupID   string   `json:"lookup_id,omitempty"`
	SealedDesc string   `json:"sealed_desc,omitempty"`
	PhonePub   string   `json:"phone_pub,omitempty"`
	EphPub     string   `json:"eph_pub,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	Nonce      string   `json:"nonce,omitempty"`
	Proof      string   `json:"proof,omitempty"`
	LANEps     []string `json:"lan_eps,omitempty"`
	WANEps     []string `json:"wan_eps,omitempty"`
	ConnMode   string   `json:"conn_mode,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type PairDescriptor struct {
	PCPub    string   `json:"pk"`
	LANEps   []string `json:"lan"`
	WANEps   []string `json:"wan"`
	Hostname string   `json:"host"`
}

type ControlMsg struct {
	Cmd            string  `json:"cmd"`
	ROIX           float64 `json:"rx,omitempty"`
	ROIY           float64 `json:"ry,omitempty"`
	ROIW           float64 `json:"rw,omitempty"`
	ROIH           float64 `json:"rh,omitempty"`
	ClientSentMS   int64   `json:"ts,omitempty"`
	ForceKeyframe  bool    `json:"kf,omitempty"`
	MaxWidth       int     `json:"mw,omitempty"`
	PreferredJPEGQ int     `json:"jq,omitempty"`
}

type TelemetryMsg struct {
	LANEndpoints []string `json:"lan_eps,omitempty"`
	WANEndpoints []string `json:"wan_eps,omitempty"`
}

func PackVideoFrame(jpeg []byte) []byte {
	buf := make([]byte, 1+len(jpeg))
	buf[0] = FrameVideo
	copy(buf[1:], jpeg)
	return buf
}

func ParseMouseFrame(b []byte) (flags byte, normX, normY uint16, wheel int16, err error) {
	if len(b) < 8 || b[0] != FrameInputMouse {
		return 0, 0, 0, 0, errors.New("invalid mouse frame")
	}
	flags = b[1]
	normX = binary.BigEndian.Uint16(b[2:4])
	normY = binary.BigEndian.Uint16(b[4:6])
	wheel = int16(binary.BigEndian.Uint16(b[6:8]))
	return flags, normX, normY, wheel, nil
}

func PackControlMsg(c ControlMsg) []byte {
	jb, _ := json.Marshal(c)
	out := make([]byte, 1+len(jb))
	out[0] = FrameControl
	copy(out[1:], jb)
	return out
}

func PackTelemetryMsg(t TelemetryMsg) []byte {
	jb, _ := json.Marshal(t)
	out := make([]byte, 1+len(jb))
	out[0] = FrameTelemetry
	copy(out[1:], jb)
	return out
}
