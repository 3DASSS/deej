package win

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modgdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procSHDefExtractIcon = modshell32.NewProc("SHDefExtractIconW")
	procGetIconInfo      = moduser32.NewProc("GetIconInfo")
	procDestroyIcon      = moduser32.NewProc("DestroyIcon")
	procGetDC            = moduser32.NewProc("GetDC")
	procReleaseDC        = moduser32.NewProc("ReleaseDC")
	procGetObject        = modgdi32.NewProc("GetObjectW")
	procGetDIBits        = modgdi32.NewProc("GetDIBits")
	procDeleteObject     = modgdi32.NewProc("DeleteObject")
)

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  windows.Handle
	HbmColor windows.Handle
}

type BITMAP struct {
	Type       int32
	Width      int32
	Height     int32
	WidthBytes int32
	Planes     uint16
	BitsPixel  uint16
	Bits       uintptr
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapInfo reserves room after the header for the three color masks
// GetDIBits may write for BI_BITFIELDS formats
type bitmapInfo struct {
	Header BITMAPINFOHEADER
	colors [3]uint32 //nolint:unused
}

// SHDefExtractIcon extracts the first icon of an executable or icon file,
// rendered at the requested pixel size
func SHDefExtractIcon(path string, size int) (icon windows.Handle, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	hr, _, _ := procSHDefExtractIcon.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		0,
		uintptr(unsafe.Pointer(&icon)),
		0,
		uintptr(uint32(size)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("SHDefExtractIcon: hr=0x%x", hr)
	}
	if icon == 0 {
		return 0, errors.New("no icon in file")
	}

	return icon, nil
}

func GetIconInfo(icon windows.Handle) (info ICONINFO, err error) {
	r1, _, lastErr := procGetIconInfo.Call(uintptr(icon), uintptr(unsafe.Pointer(&info)))

	if r1 == 0 {
		err = lastErr
	}

	return
}

func DestroyIcon(icon windows.Handle) {
	_, _, _ = procDestroyIcon.Call(uintptr(icon))
}

func DeleteObject(object windows.Handle) {
	_, _, _ = procDeleteObject.Call(uintptr(object))
}

func GetDC(hwnd windows.HWND) (hdc windows.Handle, err error) {
	r1, _, _ := procGetDC.Call(uintptr(hwnd))
	if r1 == 0 {
		return 0, errors.New("GetDC failed")
	}

	return windows.Handle(r1), nil
}

func ReleaseDC(hwnd windows.HWND, hdc windows.Handle) {
	_, _, _ = procReleaseDC.Call(uintptr(hwnd), uintptr(hdc))
}

// GetBitmapInfo returns the dimensions and format of a GDI bitmap handle
func GetBitmapInfo(bitmap windows.Handle) (bm BITMAP, err error) {
	r1, _, lastErr := procGetObject.Call(
		uintptr(bitmap),
		unsafe.Sizeof(bm),
		uintptr(unsafe.Pointer(&bm)),
	)

	if r1 == 0 {
		err = lastErr
	}

	return
}

// GetDIBits32 reads the pixels of a GDI bitmap as top-down 32bpp BGRA data
func GetDIBits32(hdc windows.Handle, bitmap windows.Handle, width int, height int) ([]byte, error) {
	info := bitmapInfo{Header: BITMAPINFOHEADER{
		Size:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		Width:    int32(width),
		Height:   -int32(height), // negative height = top-down rows
		Planes:   1,
		BitCount: 32,
	}}

	buf := make([]byte, width*height*4)
	r1, _, lastErr := procGetDIBits.Call(
		uintptr(hdc),
		uintptr(bitmap),
		0,
		uintptr(uint32(height)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&info)),
		0, // DIB_RGB_COLORS
	)
	if r1 == 0 {
		return nil, lastErr
	}

	return buf, nil
}
