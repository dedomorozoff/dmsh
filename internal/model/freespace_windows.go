//go:build windows

package model

import "golang.org/x/sys/windows"

func availableBytes(dir string) (int64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, false
	}
	return int64(free), true
}
