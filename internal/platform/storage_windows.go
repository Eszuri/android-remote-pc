package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modCrypt32             = syscall.NewLazyDLL("crypt32.dll")
	modKernel32            = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modKernel32.NewProc("LocalFree")
)

const cryptProtectUIForbidden = 0x1

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func makeBlob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func SealDPAPI(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("empty dpapi input")
	}
	in := makeBlob(plain)
	var out dataBlob
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	res := unsafe.Slice(out.pbData, out.cbData)
	cp := make([]byte, len(res))
	copy(cp, res)
	return cp, nil
}

func OpenDPAPI(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, errors.New("empty dpapi ciphertext")
	}
	in := makeBlob(cipher)
	var out dataBlob
	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	res := unsafe.Slice(out.pbData, out.cbData)
	cp := make([]byte, len(res))
	copy(cp, res)
	return cp, nil
}

func SaveProtectedFile(path string, plain []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	enc, err := SealDPAPI(plain)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadProtectedFile(path string) ([]byte, error) {
	enc, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return OpenDPAPI(enc)
}
