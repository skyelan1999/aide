//go:build windows

package server

import (
	"os"

	"golang.org/x/sys/windows"
)

func isHardlinked(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) != nil {
		return false
	}
	return info.NumberOfLinks > 1
}
