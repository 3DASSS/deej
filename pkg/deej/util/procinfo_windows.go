package util

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GetProcessFileDescription returns the FileDescription version-info string
// of the executable behind the given process (e.g. "Google Chrome" for
// chrome.exe), for use as a friendly display name
func GetProcessFileDescription(pid uint32) (string, error) {
	path, err := GetProcessImagePath(pid)
	if err != nil {
		return "", err
	}

	return getFileDescription(path)
}

// GetProcessImagePath returns the full path of the executable behind the
// given process
func GetProcessImagePath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", fmt.Errorf("open process: %w", err)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", fmt.Errorf("query process image name: %w", err)
	}

	return windows.UTF16ToString(buf[:size]), nil
}

func getFileDescription(path string) (string, error) {
	infoSize, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil {
		return "", fmt.Errorf("get version info size: %w", err)
	}

	info := make([]byte, infoSize)
	if err := windows.GetFileVersionInfo(path, 0, infoSize, unsafe.Pointer(&info[0])); err != nil {
		return "", fmt.Errorf("get version info: %w", err)
	}

	// try the file's own translation table first, then the common
	// US English + Unicode fallback some executables rely on
	subBlocks := []string{`\StringFileInfo\040904B0\FileDescription`}

	var translation *uint32
	var translationLen uint32
	if err := windows.VerQueryValue(
		unsafe.Pointer(&info[0]),
		`\VarFileInfo\Translation`,
		unsafe.Pointer(&translation),
		&translationLen,
	); err == nil && translationLen >= 4 {
		// each translation entry is a language ID + codepage pair
		subBlocks = append([]string{fmt.Sprintf(
			`\StringFileInfo\%04x%04x\FileDescription`,
			*translation&0xffff,
			*translation>>16,
		)}, subBlocks...)
	}

	for _, subBlock := range subBlocks {
		var descPtr *uint16
		var descLen uint32
		if err := windows.VerQueryValue(
			unsafe.Pointer(&info[0]),
			subBlock,
			unsafe.Pointer(&descPtr),
			&descLen,
		); err != nil || descLen == 0 {
			continue
		}

		if desc := windows.UTF16PtrToString(descPtr); desc != "" {
			return desc, nil
		}
	}

	return "", errors.New("no file description")
}
