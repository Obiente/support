package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/obiente/support/internal/cryptobox"
)

var objectKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}\.enc$`)

type Objects interface {
	Put(key, reportID string, plaintext []byte) error
	Delete(key string) error
}

type FileObjects struct {
	root string
	box  *cryptobox.Box
}

func NewFileObjects(root string, box *cryptobox.Box) (*FileObjects, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create private object root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("protect private object root: %w", err)
	}
	return &FileObjects{root: root, box: box}, nil
}

func (objects *FileObjects) Put(key, reportID string, plaintext []byte) error {
	if !objectKeyPattern.MatchString(key) {
		return errors.New("invalid private object key")
	}
	ciphertext, err := objects.box.Seal(plaintext, []byte(reportID+":diagnostics"))
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(objects.root, ".upload-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(ciphertext); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, filepath.Join(objects.root, key))
}

func (objects *FileObjects) Delete(key string) error {
	if !objectKeyPattern.MatchString(key) {
		return errors.New("invalid private object key")
	}
	err := os.Remove(filepath.Join(objects.root, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type MemoryObjects struct {
	Values map[string][]byte
}

func NewMemoryObjects() *MemoryObjects { return &MemoryObjects{Values: make(map[string][]byte)} }

func (objects *MemoryObjects) Put(key, _ string, plaintext []byte) error {
	objects.Values[key] = append([]byte(nil), plaintext...)
	return nil
}

func (objects *MemoryObjects) Delete(key string) error {
	delete(objects.Values, key)
	return nil
}
