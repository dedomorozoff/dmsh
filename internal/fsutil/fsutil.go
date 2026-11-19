// Package fsutil содержит запись служебных файлов dmsh с закрытыми
// правами доступа. В конфиге лежит пароль прокси, в истории и аудите —
// команды пользователя: читать их должен только владелец.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// WritePrivate создаёт каталог (0700) и пишет файл с правами 0600.
// Существующий файл тоже подтягивается к 0600: права 0644, выставленные
// прошлой версией dmsh, иначе остались бы навсегда.
func WritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	// Tighten existing directory permissions as well.
	_ = os.Chmod(dir, dirPerm)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// На Windows chmod меняет только флаг read-only, но вызов безвреден.
	_ = os.Chmod(path, filePerm)
	return nil
}

// AppendPrivate открывает файл на дозапись, создавая его с правами 0600.
func AppendPrivate(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", dir, err)
	}
	_ = os.Chmod(dir, dirPerm)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, filePerm)
	return f, nil
}

// DirPerm и FilePerm — для тестов, которым нужно проверить политику прав.
const (
	DirPerm  = dirPerm
	FilePerm = filePerm
)
