package platform

import (
	"unicode/utf16"
	"unsafe"

	"arpc/internal/protocol"
)

var procSendInput = modUser32.NewProc("SendInput")

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseEventMove       = 0x0001
	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800
	mouseEventAbsolute   = 0x8000

	keyEventExtended = 0x0001
	keyEventKeyUp    = 0x0002
	keyEventUnicode  = 0x0004

	vkControl = 0x11
	vkMenu    = 0x12
	vkShift   = 0x10
	vkLWin    = 0x5B
)

type mouseInputRecord struct {
	typ         uint32
	_           uint32
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	_           uint32
	dwExtraInfo uintptr
}

type keybdInputRecord struct {
	typ         uint32
	_           uint32
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	_           uint32
	dwExtraInfo uintptr
	_pad        [8]byte
}

func HandleMouse(flags byte, normX, normY uint16, wheel int16) {
	var events []mouseInputRecord
	if flags&protocol.MouseMove != 0 {
		events = append(events, mouseInputRecord{
			typ:     inputMouse,
			dx:      int32(normX),
			dy:      int32(normY),
			dwFlags: mouseEventMove | mouseEventAbsolute,
		})
	}
	if flags&protocol.MouseLeftDown != 0 {
		events = append(events, mouseInputRecord{typ: inputMouse, dwFlags: mouseEventLeftDown})
	}
	if flags&protocol.MouseLeftUp != 0 {
		events = append(events, mouseInputRecord{typ: inputMouse, dwFlags: mouseEventLeftUp})
	}
	if flags&protocol.MouseRightDown != 0 {
		events = append(events, mouseInputRecord{typ: inputMouse, dwFlags: mouseEventRightDown})
	}
	if flags&protocol.MouseRightUp != 0 {
		events = append(events, mouseInputRecord{typ: inputMouse, dwFlags: mouseEventRightUp})
	}
	if flags&protocol.MouseMidClick != 0 {
		events = append(events,
			mouseInputRecord{typ: inputMouse, dwFlags: mouseEventMiddleDown},
			mouseInputRecord{typ: inputMouse, dwFlags: mouseEventMiddleUp},
		)
	}
	if flags&protocol.MouseWheel != 0 && wheel != 0 {
		events = append(events, mouseInputRecord{
			typ:       inputMouse,
			mouseData: uint32(int32(wheel)),
			dwFlags:   mouseEventWheel,
		})
	}
	if len(events) > 0 {
		_, _, _ = procSendInput.Call(
			uintptr(len(events)),
			uintptr(unsafe.Pointer(&events[0])),
			unsafe.Sizeof(events[0]),
		)
	}
}

func extKeyFlag(vk uint16) uint32 {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E, 0x5B, 0x5C:
		return keyEventExtended
	}
	return 0
}

func HandleKey(flags byte, vk uint16, text string) {
	var records []keybdInputRecord

	if flags&protocol.KeyUnicode != 0 && len(text) > 0 {
		u16 := utf16.Encode([]rune(text))
		for _, ch := range u16 {
			records = append(records,
				keybdInputRecord{typ: inputKeyboard, wScan: ch, dwFlags: keyEventUnicode},
				keybdInputRecord{typ: inputKeyboard, wScan: ch, dwFlags: keyEventUnicode | keyEventKeyUp},
			)
		}
	} else if flags&protocol.KeyCombo != 0 {
		mods := byte(vk >> 8)
		baseVK := vk & 0xFF
		var activeMods []uint16
		if mods&1 != 0 {
			activeMods = append(activeMods, vkControl)
		}
		if mods&2 != 0 {
			activeMods = append(activeMods, vkMenu)
		}
		if mods&4 != 0 {
			activeMods = append(activeMods, vkShift)
		}
		if mods&8 != 0 {
			activeMods = append(activeMods, vkLWin)
		}
		for _, m := range activeMods {
			records = append(records, keybdInputRecord{typ: inputKeyboard, wVk: m, dwFlags: extKeyFlag(m)})
		}
		if baseVK != 0 {
			ef := extKeyFlag(baseVK)
			records = append(records,
				keybdInputRecord{typ: inputKeyboard, wVk: baseVK, dwFlags: ef},
				keybdInputRecord{typ: inputKeyboard, wVk: baseVK, dwFlags: ef | keyEventKeyUp},
			)
		}
		for i := len(activeMods) - 1; i >= 0; i-- {
			m := activeMods[i]
			records = append(records, keybdInputRecord{typ: inputKeyboard, wVk: m, dwFlags: extKeyFlag(m) | keyEventKeyUp})
		}
	} else {
		ef := extKeyFlag(vk)
		if flags&protocol.KeyDown != 0 {
			records = append(records, keybdInputRecord{typ: inputKeyboard, wVk: vk, dwFlags: ef})
		}
		if flags&protocol.KeyUp != 0 {
			records = append(records, keybdInputRecord{typ: inputKeyboard, wVk: vk, dwFlags: ef | keyEventKeyUp})
		}
	}

	if len(records) > 0 {
		_, _, _ = procSendInput.Call(
			uintptr(len(records)),
			uintptr(unsafe.Pointer(&records[0])),
			unsafe.Sizeof(records[0]),
		)
	}
}
