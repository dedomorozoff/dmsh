//go:build !unix && !windows

package model

func availableBytes(string) (int64, bool) { return 0, false }
