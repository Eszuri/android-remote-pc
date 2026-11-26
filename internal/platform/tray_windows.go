package platform

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	modShell32           = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIconW = modShell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = modShell32.NewProc("ShellExecuteW")
	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procLoadIconW        = modUser32.NewProc("LoadIconW")
	procLoadImageW       = modUser32.NewProc("LoadImageW")
	procCreatePopupMenu  = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW      = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu   = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu      = modUser32.NewProc("DestroyMenu")
	procGetCursorPos     = modUser32.NewProc("GetCursorPos")
	procSetForegroundWin = modUser32.NewProc("SetForegroundWindow")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmTrayIcon    = 0x8001
	nimAdd        = 0x00000000
	nimDelete     = 0x00000002
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	idiApp        = 32512
	appIconResID  = 1
	imageIcon     = 1
	smCxIcon      = 11
	smCyIcon      = 12
	smCxSmIcon    = 49
	smCySmIcon    = 50
	mfString      = 0x00000000
	mfChecked     = 0x00000008
	mfSeparator   = 0x00000800
	tpmRightBtn   = 0x0002
	tpmNonotify   = 0x0080
	tpmReturnCmd  = 0x0100
	swShowNormal  = 1
	menuOpenPanel = 1001
	menuQuitApp   = 1002
	menuUnbindApp = 1003
	menuAutostart = 1004
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type notifyIconDataW struct {
	cbSize           uint32
	_                uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	_                uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type point struct {
	x, y int32
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	_       uint32
}

var (
	trayPanelURL string
	trayOnUnbind func()
	trayHWND     uintptr
	trayNID      notifyIconDataW
)

func OpenBrowser(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(url)
	_, _, _ = procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, swShowNormal)
}

func StopTray() {
	if trayHWND != 0 {
		_, _, _ = procPostMessageW.Call(trayHWND, wmClose, 0, 0)
	}
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		ev := uint32(lParam & 0xFFFF)
		if ev == wmLButtonUp {
			OpenBrowser(trayPanelURL)
			return 0
		}
		if ev == wmRButtonUp {
			showTrayMenu(hwnd)
			return 0
		}
	case wmClose, wmDestroy:
		_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayNID)))
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func showTrayMenu(hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	lblOpen, _ := syscall.UTF16PtrFromString("Open ARPC")
	lblAuto, _ := syscall.UTF16PtrFromString("Auto Launch When Start up")
	lblUnbind, _ := syscall.UTF16PtrFromString("Unbind Connection")
	lblQuit, _ := syscall.UTF16PtrFromString("Exit ARPC")
	autoFlags := uintptr(mfString)
	if AutostartEnabled() {
		autoFlags |= mfChecked
	}
	_, _, _ = procAppendMenuW.Call(hMenu, mfString, menuOpenPanel, uintptr(unsafe.Pointer(lblOpen)))
	_, _, _ = procAppendMenuW.Call(hMenu, autoFlags, menuAutostart, uintptr(unsafe.Pointer(lblAuto)))
	_, _, _ = procAppendMenuW.Call(hMenu, mfString, menuUnbindApp, uintptr(unsafe.Pointer(lblUnbind)))
	_, _, _ = procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	_, _, _ = procAppendMenuW.Call(hMenu, mfString, menuQuitApp, uintptr(unsafe.Pointer(lblQuit)))

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWin.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(hMenu, tpmRightBtn|tpmNonotify|tpmReturnCmd, uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)
	switch cmd {
	case menuOpenPanel:
		OpenBrowser(trayPanelURL)
	case menuAutostart:
		SetAutostart(!AutostartEnabled())
	case menuUnbindApp:
		if trayOnUnbind != nil {
			go trayOnUnbind()
		}
	case menuQuitApp:
		StopTray()
	}
}

func loadAppIcon(hInst uintptr, cxMetric, cyMetric int) uintptr {
	cx, _, _ := procGetSystemMetrics.Call(uintptr(cxMetric))
	cy, _, _ := procGetSystemMetrics.Call(uintptr(cyMetric))
	if cx > 0 && cy > 0 {
		if h, _, _ := procLoadImageW.Call(hInst, appIconResID, imageIcon, cx, cy, 0); h != 0 {
			return h
		}
	}
	if h, _, _ := procLoadIconW.Call(hInst, appIconResID); h != 0 {
		return h
	}
	h, _, _ := procLoadIconW.Call(0, idiApp)
	return h
}

func RunTray(tooltip, panelURL string, onUnbind func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	trayPanelURL = panelURL
	trayOnUnbind = onUnbind
	hInst, _, _ := procGetModuleHandleW.Call(0)
	clsName, _ := syscall.UTF16PtrFromString("arpcTrayClass")
	hIconBig := loadAppIcon(hInst, smCxIcon, smCyIcon)
	hIconSm := loadAppIcon(hInst, smCxSmIcon, smCySmIcon)

	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(trayWndProc)
	wc.hInstance = hInst
	wc.hIcon = hIconBig
	wc.hIconSm = hIconSm
	wc.lpszClassName = clsName
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwndMessage := ^uintptr(2)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(clsName)), uintptr(unsafe.Pointer(clsName)),
		0, 0, 0, 0, 0, hwndMessage, 0, hInst, 0,
	)
	if hwnd == 0 {
		return
	}
	trayHWND = hwnd

	trayNID = notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(trayNID)),
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayIcon,
		hIcon:            hIconSm,
	}
	tipUTF16, _ := syscall.UTF16FromString(tooltip)
	copy(trayNID.szTip[:], tipUTF16)
	_, _, _ = procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&trayNID)))

	var m winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayNID)))
}
