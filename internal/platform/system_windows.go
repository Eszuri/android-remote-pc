package platform

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32Sys       = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW  = modAdvapi32Sys.NewProc("RegCreateKeyExW")
	procRegSetValueExW   = modAdvapi32Sys.NewProc("RegSetValueExW")
	procRegDeleteValueW  = modAdvapi32Sys.NewProc("RegDeleteValueW")
	procRegQueryValueExW = modAdvapi32Sys.NewProc("RegQueryValueExW")
	procRegCloseKeySys   = modAdvapi32Sys.NewProc("RegCloseKey")
	procCreateMutexW     = modKernel32.NewProc("CreateMutexW")
	procMessageBoxW      = modUser32.NewProc("MessageBoxW")
)

const (
	hkeyCurrentUser  = 0x80000001
	keyAllAccess     = 0xF003F
	regSZ            = 1
	errAlreadyExists = 183
	autostartKey     = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartName    = "ARPC"
)

func AcquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("ARPC_SingleInstance")
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errAlreadyExists {
		return false
	}
	return true
}

func ShowInfo(title, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x40)
}

func openRunKey() (syscall.Handle, bool) {
	key, _ := syscall.UTF16PtrFromString(autostartKey)
	var h syscall.Handle
	r, _, _ := procRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(key)), 0, 0, 0, keyAllAccess, 0, uintptr(unsafe.Pointer(&h)), 0)
	return h, r == 0
}

func AutostartEnabled() bool {
	h, ok := openRunKey()
	if !ok {
		return false
	}
	defer procRegCloseKeySys.Call(uintptr(h))
	name, _ := syscall.UTF16PtrFromString(autostartName)
	r, _, _ := procRegQueryValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, 0, 0, 0)
	return r == 0
}

func SetAutostart(on bool) {
	h, ok := openRunKey()
	if !ok {
		return
	}
	defer procRegCloseKeySys.Call(uintptr(h))
	name, _ := syscall.UTF16PtrFromString(autostartName)
	if !on {
		_, _, _ = procRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(name)))
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	val, _ := syscall.UTF16FromString(`"` + exe + `" --startup`)
	_, _, _ = procRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, regSZ, uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
}
