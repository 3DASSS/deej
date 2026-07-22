package util

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	"golang.org/x/sys/windows"

	"github.com/nik9play/deej/pkg/win"
)

const processIconSize = 48

const ProcessIconsSupported = true

// GetFileIconPNG returns the icon of the given executable, encoded as a PNG
func GetFileIconPNG(path string) ([]byte, error) {
	icon, err := win.SHDefExtractIcon(path, processIconSize)
	if err != nil {
		return nil, fmt.Errorf("extract icon: %w", err)
	}
	defer win.DestroyIcon(icon)

	img, err := iconToImage(icon)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}

	return buf.Bytes(), nil
}

func iconToImage(icon windows.Handle) (image.Image, error) {
	info, err := win.GetIconInfo(icon)
	if err != nil {
		return nil, fmt.Errorf("get icon info: %w", err)
	}
	defer func() {
		if info.HbmColor != 0 {
			win.DeleteObject(info.HbmColor)
		}
		if info.HbmMask != 0 {
			win.DeleteObject(info.HbmMask)
		}
	}()

	if info.HbmColor == 0 {
		return nil, errors.New("monochrome icon")
	}

	bm, err := win.GetBitmapInfo(info.HbmColor)
	if err != nil {
		return nil, fmt.Errorf("get bitmap info: %w", err)
	}

	width, height := int(bm.Width), int(bm.Height)
	if width <= 0 || height <= 0 || width > 256 || height > 256 {
		return nil, fmt.Errorf("unexpected icon dimensions %dx%d", width, height)
	}

	hdc, err := win.GetDC(0)
	if err != nil {
		return nil, fmt.Errorf("get dc: %w", err)
	}
	defer win.ReleaseDC(0, hdc)

	pixels, err := win.GetDIBits32(hdc, info.HbmColor, width, height)
	if err != nil {
		return nil, fmt.Errorf("get color bits: %w", err)
	}

	// old-style icons have no alpha channel; their transparency lives in the
	// separate AND mask instead
	hasAlpha := false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			hasAlpha = true
			break
		}
	}

	var mask []byte
	if !hasAlpha && info.HbmMask != 0 {
		mask, _ = win.GetDIBits32(hdc, info.HbmMask, width, height)
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		// DIB pixel order is BGRA
		img.Pix[i*4+0] = pixels[i*4+2]
		img.Pix[i*4+1] = pixels[i*4+1]
		img.Pix[i*4+2] = pixels[i*4+0]
		switch {
		case hasAlpha:
			img.Pix[i*4+3] = pixels[i*4+3]
		case mask != nil && mask[i*4] != 0: // white mask pixel = transparent
			img.Pix[i*4+3] = 0
		default:
			img.Pix[i*4+3] = 0xff
		}
	}

	return img, nil
}
