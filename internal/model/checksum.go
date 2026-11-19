package model

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// SHA256SidecarPath returns the conventional checksum sidecar path for a model file.
func SHA256SidecarPath(modelPath string) string {
	return modelPath + ".sha256"
}

// ReadSHA256Sidecar reads the expected SHA256 hash from a sidecar file.
// The file may contain just the hash or the common "<hash>  <name>" format.
func ReadSHA256Sidecar(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) == 64 && isHex(sum) {
			return sum, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no sha256 checksum found in %s", path)
}

// VerifySHA256 checks file contents against an expected hex digest.
func VerifySHA256(path, want string) error {
	want = strings.ToLower(strings.TrimSpace(want))
	if len(want) != 64 || !isHex(want) {
		return fmt.Errorf("invalid sha256 checksum")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("sha256 mismatch: want %s, got %s", want, got)
	}
	return nil
}

// VerifySHA256Sidecar verifies modelPath against its .sha256 sidecar.
// If the sidecar does not exist, (false, nil) is returned.
func VerifySHA256Sidecar(modelPath string) (checked bool, err error) {
	want, err := ReadSHA256Sidecar(SHA256SidecarPath(modelPath))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := VerifySHA256(modelPath, want); err != nil {
		return true, err
	}
	return true, nil
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
