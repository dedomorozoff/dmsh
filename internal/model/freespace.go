package model

import (
	"fmt"
	"path/filepath"
)

// freeSpaceReserve keeps a small margin for filesystem metadata so a download
// that technically fits does not fail at the last block.
const freeSpaceReserve int64 = 64 << 20

// checkFreeSpace refuses a download that cannot fit next to its destination.
// It is best-effort: platforms without a free-space API skip the check.
func checkFreeSpace(dest string, need int64) error {
	if need <= 0 {
		return nil
	}
	free, ok := availableBytes(filepath.Dir(dest))
	if !ok || free < 0 {
		return nil
	}
	if free < need+freeSpaceReserve {
		return fmt.Errorf("not enough free space in %s: need ~%d MB, have %d MB",
			filepath.Dir(dest), need>>20, free>>20)
	}
	return nil
}
