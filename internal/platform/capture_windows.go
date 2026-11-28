package platform

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modUser32                     = syscall.NewLazyDLL("user32.dll")
	modGDI32                      = syscall.NewLazyDLL("gdi32.dll")
	procSetProcessDpiAwarenessCtx = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetSystemMetrics          = modUser32.NewProc("GetSystemMetrics")
	procGetDC                     = modUser32.NewProc("GetDC")
	procReleaseDC                 = modUser32.NewProc("ReleaseDC")
	procCreateCompatibleDC        = modGDI32.NewProc("CreateCompatibleDC")
	procDeleteDC                  = modGDI32.NewProc("DeleteDC")
	procCreateDIBSection          = modGDI32.NewProc("CreateDIBSection")
	procSelectObject              = modGDI32.NewProc("SelectObject")
	procDeleteObject              = modGDI32.NewProc("DeleteObject")
	procBitBlt                    = modGDI32.NewProc("BitBlt")
	procStretchBlt                = modGDI32.NewProc("StretchBlt")
	procSetStretchBltMode         = modGDI32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx             = modGDI32.NewProc("SetBrushOrgEx")
)

const (
	smCxScreen = 0
	smCyScreen = 1
	srccopy    = 0x00CC0020
	biRGB      = 0
	halftone   = 4
)

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [1]uint32
}

var capPool struct {
	mu      sync.Mutex
	w, h    int
	hdcMem  uintptr
	hbmp    uintptr
	oldObj  uintptr
	bitsPtr *byte
	ycbcr   *image.YCbCr
	jpegBuf bytes.Buffer
}

func init() {
	if procSetProcessDpiAwarenessCtx.Find() == nil {
		_, _, _ = procSetProcessDpiAwarenessCtx.Call(^uintptr(3))
	}
}

func ScreenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(smCxScreen)
	h, _, _ := procGetSystemMetrics.Call(smCyScreen)
	if w <= 0 || h <= 0 {
		return 1920, 1080
	}
	return int(w), int(h)
}

type CaptureResult struct {
	Unchanged bool
	Hash      uint64
	JPEG      []byte
}

func ensurePoolLocked(hdcScreen uintptr, dstW, dstH int) bool {
	if capPool.hdcMem != 0 && capPool.w == dstW && capPool.h == dstH && capPool.bitsPtr != nil {
		return true
	}
	if capPool.oldObj != 0 && capPool.hdcMem != 0 {
		procSelectObject.Call(capPool.hdcMem, capPool.oldObj)
	}
	if capPool.hbmp != 0 {
		procDeleteObject.Call(capPool.hbmp)
	}
	if capPool.hdcMem != 0 {
		procDeleteDC.Call(capPool.hdcMem)
	}
	capPool.hdcMem, capPool.hbmp, capPool.oldObj, capPool.bitsPtr = 0, 0, 0, nil

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return false
	}
	procSetStretchBltMode.Call(hdcMem, halftone)
	procSetBrushOrgEx.Call(hdcMem, 0, 0, 0)

	var bi bitmapInfo
	bi.bmiHeader.biSize = uint32(unsafe.Sizeof(bi.bmiHeader))
	bi.bmiHeader.biWidth = int32(dstW)
	bi.bmiHeader.biHeight = -int32(dstH)
	bi.bmiHeader.biPlanes = 1
	bi.bmiHeader.biBitCount = 32
	bi.bmiHeader.biCompression = biRGB

	var bitsPtr *byte
	hbmp, _, _ := procCreateDIBSection.Call(
		hdcMem,
		uintptr(unsafe.Pointer(&bi)),
		0,
		uintptr(unsafe.Pointer(&bitsPtr)),
		0,
		0,
	)
	if hbmp == 0 || bitsPtr == nil {
		procDeleteDC.Call(hdcMem)
		return false
	}
	oldObj, _, _ := procSelectObject.Call(hdcMem, hbmp)

	capPool.w = dstW
	capPool.h = dstH
	capPool.hdcMem = hdcMem
	capPool.hbmp = hbmp
	capPool.oldObj = oldObj
	capPool.bitsPtr = bitsPtr
	capPool.ycbcr = image.NewYCbCr(image.Rect(0, 0, dstW, dstH), image.YCbCrSubsampleRatio420)
	return true
}

func CaptureROI(rx, ry, rw, rh float64, maxWidth int, quality int, prevHash uint64, forceFull bool) (*CaptureResult, error) {
	scrW, scrH := ScreenSize()
	if rw <= 0.05 || rw > 1.0 {
		rw = 1.0
	}
	if rh <= 0.05 || rh > 1.0 {
		rh = 1.0
	}
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	if rx+rw > 1.0 {
		rx = 1.0 - rw
	}
	if ry+rh > 1.0 {
		ry = 1.0 - rh
	}

	cropX := int(rx * float64(scrW))
	cropY := int(ry * float64(scrH))
	cropW := int(rw * float64(scrW))
	cropH := int(rh * float64(scrH))
	if cropW < 64 {
		cropW = 64
	}
	if cropH < 64 {
		cropH = 64
	}
	if cropX+cropW > scrW {
		cropW = scrW - cropX
	}
	if cropY+cropH > scrH {
		cropH = scrH - cropY
	}

	dstW, dstH := cropW, cropH
	if maxWidth > 0 && dstW > maxWidth {
		dstW = maxWidth
		dstH = (cropH * dstW) / cropW
	}
	dstW = (dstW + 1) &^ 1
	dstH = (dstH + 1) &^ 1
	if dstW < 2 {
		dstW = 2
	}
	if dstH < 2 {
		dstH = 2
	}
	if quality < 35 {
		quality = 82
	}
	if quality > 95 {
		quality = 95
	}

	capPool.mu.Lock()
	defer capPool.mu.Unlock()

	if !captureIntoPoolLocked(cropX, cropY, cropW, cropH, dstW, dstH) {
		return encodeFallbackLocked(dstW, dstH, quality)
	}

	bgra := unsafe.Slice(capPool.bitsPtr, dstW*dstH*4)
	h := fastHashBGRA(bgra, cropX, cropY, dstW, quality)
	if !forceFull && h == prevHash && prevHash != 0 {
		return &CaptureResult{Unchanged: true, Hash: h}, nil
	}

	bgraToYCbCr420(bgra, dstW, dstH, capPool.ycbcr)
	capPool.jpegBuf.Reset()
	if err := jpeg.Encode(&capPool.jpegBuf, capPool.ycbcr, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	outJPEG := make([]byte, capPool.jpegBuf.Len())
	copy(outJPEG, capPool.jpegBuf.Bytes())

	return &CaptureResult{Hash: h, JPEG: outJPEG}, nil
}

func captureIntoPoolLocked(srcX, srcY, srcW, srcH, dstW, dstH int) bool {
	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return false
	}
	defer procReleaseDC.Call(0, hdcScreen)

	if !ensurePoolLocked(hdcScreen, dstW, dstH) {
		return false
	}
	if srcW == dstW && srcH == dstH {
		ret, _, _ := procBitBlt.Call(
			capPool.hdcMem, 0, 0, uintptr(dstW), uintptr(dstH),
			hdcScreen, uintptr(srcX), uintptr(srcY), srccopy,
		)
		return ret != 0
	}
	ret, _, _ := procStretchBlt.Call(
		capPool.hdcMem, 0, 0, uintptr(dstW), uintptr(dstH),
		hdcScreen, uintptr(srcX), uintptr(srcY), uintptr(srcW), uintptr(srcH),
		srccopy,
	)
	return ret != 0
}

func fastHashBGRA(bgra []byte, x, y, mw, q int) uint64 {
	h := uint64(14695981039346656037)
	const prime = 1099511628211
	h = (h ^ uint64(x<<16|y)) * prime
	h = (h ^ uint64(mw<<8|q)) * prime
	for i := 0; i+2 < len(bgra); i += 48 {
		v := uint64(bgra[i]) | uint64(bgra[i+1])<<8 | uint64(bgra[i+2])<<16
		h = (h ^ v) * prime
	}
	return h
}

func bgraToYCbCr420(bgra []byte, w, h int, dst *image.YCbCr) {
	yPlane, cbPlane, crPlane := dst.Y, dst.Cb, dst.Cr
	yStride, cStride := dst.YStride, dst.CStride

	for y := 0; y < h; y += 2 {
		row0, row1 := y*w*4, (y+1)*w*4
		yOff0, yOff1 := y*yStride, (y+1)*yStride
		cOff := (y >> 1) * cStride

		for x := 0; x < w; x += 2 {
			p00 := row0 + x*4
			p01 := p00 + 4
			p10 := row1 + x*4
			p11 := p10 + 4

			b0, g0, r0 := int(bgra[p00]), int(bgra[p00+1]), int(bgra[p00+2])
			b1, g1, r1 := int(bgra[p01]), int(bgra[p01+1]), int(bgra[p01+2])
			b2, g2, r2 := int(bgra[p10]), int(bgra[p10+1]), int(bgra[p10+2])
			b3, g3, r3 := int(bgra[p11]), int(bgra[p11+1]), int(bgra[p11+2])

			yPlane[yOff0+x] = uint8((77*r0 + 150*g0 + 29*b0) >> 8)
			yPlane[yOff0+x+1] = uint8((77*r1 + 150*g1 + 29*b1) >> 8)
			yPlane[yOff1+x] = uint8((77*r2 + 150*g2 + 29*b2) >> 8)
			yPlane[yOff1+x+1] = uint8((77*r3 + 150*g3 + 29*b3) >> 8)

			rAvg := (r0 + r1 + r2 + r3) >> 2
			gAvg := (g0 + g1 + g2 + g3) >> 2
			bAvg := (b0 + b1 + b2 + b3) >> 2

			cb := ((-43*rAvg - 85*gAvg + 128*bAvg) >> 8) + 128
			cr := ((128*rAvg - 107*gAvg - 21*bAvg) >> 8) + 128
			if cb < 0 {
				cb = 0
			} else if cb > 255 {
				cb = 255
			}
			if cr < 0 {
				cr = 0
			} else if cr > 255 {
				cr = 255
			}
			cx := x >> 1
			cbPlane[cOff+cx] = uint8(cb)
			crPlane[cOff+cx] = uint8(cr)
		}
	}
}

func encodeFallbackLocked(dstW, dstH int, q int) (*CaptureResult, error) {
	if capPool.ycbcr == nil || capPool.w != dstW || capPool.h != dstH {
		capPool.w, capPool.h = dstW, dstH
		capPool.ycbcr = image.NewYCbCr(image.Rect(0, 0, dstW, dstH), image.YCbCrSubsampleRatio420)
	}
	for i := range capPool.ycbcr.Y {
		capPool.ycbcr.Y[i] = 24
	}
	for i := range capPool.ycbcr.Cb {
		capPool.ycbcr.Cb[i] = 136
		capPool.ycbcr.Cr[i] = 122
	}
	capPool.jpegBuf.Reset()
	if err := jpeg.Encode(&capPool.jpegBuf, capPool.ycbcr, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	out := make([]byte, capPool.jpegBuf.Len())
	copy(out, capPool.jpegBuf.Bytes())
	return &CaptureResult{Hash: 1, JPEG: out}, nil
}
