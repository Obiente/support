package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// StorageMarkerName is the file in the private object root that names the
// volume. It starts with a dot so it can never match an object key.
const StorageMarkerName = ".storage-id"

var storageIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrStorageMismatch means the mounted object root is not the volume the
// database was written against.
var ErrStorageMismatch = errors.New("private object root is not the volume this database was written against")

// StorageBinding describes how startup matched the object root to the database.
type StorageBinding string

const (
	StorageCreated StorageBinding = "created"
	StorageAdopted StorageBinding = "adopted"
	StorageMatched StorageBinding = "matched"
	StorageRebound StorageBinding = "rebound"
)

// StorageCheck is the result of EnsureStorageIdentity. PreviousID is set only
// when an operator override replaced the database binding.
type StorageCheck struct {
	Binding    StorageBinding
	StorageID  string
	PreviousID string
}

// EnsureStorageIdentity verifies that root is the private object volume the
// database expects. A fresh database and volume are bound to a new random ID,
// and a fresh database adopts an existing marker. When the database already
// holds an ID, a missing or different marker is refused with ErrStorageMismatch
// unless rebind is set, which is an explicit operator recovery override. The
// marker is never overwritten.
func EnsureStorageIdentity(ctx context.Context, root string, identities StorageIdentities, rebind bool, now time.Time) (StorageCheck, error) {
	marker, markerExists, err := ReadStorageMarker(root)
	if err != nil {
		return StorageCheck{}, err
	}
	expected, err := identities.StorageID(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return StorageCheck{}, fmt.Errorf("read expected storage ID: %w", err)
	}
	bound := err == nil

	switch {
	case bound && markerExists && marker == expected:
		return StorageCheck{Binding: StorageMatched, StorageID: expected}, nil
	case bound && !rebind:
		found := "no " + StorageMarkerName + " marker"
		if markerExists {
			found = "storage ID " + shortStorageID(marker)
		}
		return StorageCheck{}, fmt.Errorf("%w: %s has %s but the database expects storage ID %s; mount the original persistent volume at this path",
			ErrStorageMismatch, root, found, shortStorageID(expected))
	}

	binding := StorageAdopted
	if !markerExists {
		if marker, err = createStorageMarker(root); err != nil {
			return StorageCheck{}, err
		}
		binding = StorageCreated
	}
	if bound {
		if err := identities.RebindStorageID(ctx, marker, now); err != nil {
			return StorageCheck{}, fmt.Errorf("rebind storage ID: %w", err)
		}
		return StorageCheck{Binding: StorageRebound, StorageID: marker, PreviousID: expected}, nil
	}
	stored, err := identities.BindStorageID(ctx, marker, now)
	if err != nil {
		return StorageCheck{}, fmt.Errorf("bind storage ID: %w", err)
	}
	if stored != marker {
		return StorageCheck{}, fmt.Errorf("%w: %s has storage ID %s but the database was bound to %s concurrently",
			ErrStorageMismatch, root, shortStorageID(marker), shortStorageID(stored))
	}
	return StorageCheck{Binding: binding, StorageID: marker}, nil
}

// ReadStorageMarker returns the storage ID recorded in root, if any.
func ReadStorageMarker(root string) (string, bool, error) {
	content, err := os.ReadFile(filepath.Join(root, StorageMarkerName))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read storage marker in %s: %w", root, err)
	}
	id := strings.TrimSpace(string(content))
	if !storageIDPattern.MatchString(id) {
		return "", false, fmt.Errorf("storage marker in %s is malformed; restore it from the original volume", root)
	}
	return id, true, nil
}

// createStorageMarker writes a new random storage ID to a synced temporary file
// and links it into place, which fails instead of replacing a marker that
// appeared concurrently. In that case the existing marker wins.
func createStorageMarker(root string) (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	id := hex.EncodeToString(value)
	temporary, err := os.CreateTemp(root, ".storage-id-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create storage marker in %s: %w", root, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	_, writeErr := temporary.WriteString(id + "\n")
	chmodErr := temporary.Chmod(0o600)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, chmodErr, syncErr, closeErr); err != nil {
		return "", fmt.Errorf("write storage marker in %s: %w", root, err)
	}
	if err := os.Link(temporaryName, filepath.Join(root, StorageMarkerName)); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, exists, readErr := ReadStorageMarker(root)
			if readErr != nil {
				return "", readErr
			}
			if exists {
				return existing, nil
			}
		}
		return "", fmt.Errorf("install storage marker in %s: %w", root, err)
	}
	syncDirectory(root)
	return id, nil
}

// syncDirectory makes the marker's directory entry durable where the platform
// supports syncing a directory. Failure is not fatal: the file contents are
// already synced and the next startup reads the marker again.
func syncDirectory(root string) {
	directory, err := os.Open(root)
	if err != nil {
		return
	}
	_ = directory.Sync()
	_ = directory.Close()
}

func shortStorageID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
