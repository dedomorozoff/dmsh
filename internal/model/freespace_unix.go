//go:build unix

package model

import "golang.org/x/sys/unix"

func availableBytes(dir string) (int64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
