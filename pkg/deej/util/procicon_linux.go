package util

import "errors"

const ProcessIconsSupported = false

var errProcessIconsUnsupported = errors.New("process icons are not supported on this platform")

func GetProcessImagePath(_ uint32) (string, error) {
	return "", errProcessIconsUnsupported
}

func GetFileIconPNG(_ string) ([]byte, error) {
	return nil, errProcessIconsUnsupported
}
