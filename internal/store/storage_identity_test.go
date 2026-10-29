package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var (
	storageIDA = strings.Repeat("a", 64)
	storageIDB = strings.Repeat("b", 64)
	bindTime   = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
)

func writeMarker(t *testing.T, root, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, StorageMarkerName), []byte(id+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func markerContent(t *testing.T, root string) string {
	t.Helper()
	id, exists, err := ReadStorageMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		return ""
	}
	return id
}

func boundID(t *testing.T, identities *Memory) string {
	t.Helper()
	id, err := identities.StorageID(context.Background())
	if errors.Is(err, ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStorageIdentityBindsFreshDatabaseAndVolume(t *testing.T) {
	root := t.TempDir()
	identities := NewMemory()
	check, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if err != nil {
		t.Fatal(err)
	}
	if check.Binding != StorageCreated || !storageIDPattern.MatchString(check.StorageID) {
		t.Fatalf("fresh check = %#v", check)
	}
	if markerContent(t, root) != check.StorageID || boundID(t, identities) != check.StorageID {
		t.Fatalf("marker %q and database %q differ from %q", markerContent(t, root), boundID(t, identities), check.StorageID)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != StorageMarkerName {
		t.Fatalf("object root entries = %v, want only the marker", entries)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, StorageMarkerName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("marker mode = %v, want 0600", info.Mode().Perm())
		}
	}

	again, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if err != nil || again.Binding != StorageMatched || again.StorageID != check.StorageID {
		t.Fatalf("second start = %#v, %v; want a match", again, err)
	}
}

func TestStorageIdentityAdoptsExistingMarkerForUnboundDatabase(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, storageIDA)
	identities := NewMemory()
	check, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if err != nil {
		t.Fatal(err)
	}
	if check.Binding != StorageAdopted || check.StorageID != storageIDA || boundID(t, identities) != storageIDA {
		t.Fatalf("adopt check = %#v, database = %q", check, boundID(t, identities))
	}
	if markerContent(t, root) != storageIDA {
		t.Fatal("adoption changed the marker")
	}
}

func TestStorageIdentityMatchesBoundVolume(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, storageIDA)
	identities := NewMemory()
	if err := identities.RebindStorageID(context.Background(), storageIDA, bindTime); err != nil {
		t.Fatal(err)
	}
	check, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if err != nil || check.Binding != StorageMatched || check.StorageID != storageIDA {
		t.Fatalf("match check = %#v, %v", check, err)
	}
}

func TestStorageIdentityRefusesDifferentVolume(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, storageIDB)
	identities := NewMemory()
	if err := identities.RebindStorageID(context.Background(), storageIDA, bindTime); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if !errors.Is(err, ErrStorageMismatch) || !strings.Contains(err.Error(), root) ||
		!strings.Contains(err.Error(), "mount the original persistent volume") {
		t.Fatalf("mismatch error = %v", err)
	}
	if markerContent(t, root) != storageIDB || boundID(t, identities) != storageIDA {
		t.Fatal("a refused start changed the marker or the database binding")
	}
}

func TestStorageIdentityRefusesMissingMarkerForBoundDatabase(t *testing.T) {
	root := t.TempDir()
	identities := NewMemory()
	if err := identities.RebindStorageID(context.Background(), storageIDA, bindTime); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureStorageIdentity(context.Background(), root, identities, false, bindTime)
	if !errors.Is(err, ErrStorageMismatch) || !strings.Contains(err.Error(), "no "+StorageMarkerName) {
		t.Fatalf("missing marker error = %v", err)
	}
	if _, exists, _ := ReadStorageMarker(root); exists {
		t.Fatal("a refused start created a marker")
	}
	if boundID(t, identities) != storageIDA {
		t.Fatal("a refused start changed the database binding")
	}
}

func TestStorageIdentityOverrideRebindsToMountedVolume(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, storageIDB)
	identities := NewMemory()
	if err := identities.RebindStorageID(context.Background(), storageIDA, bindTime); err != nil {
		t.Fatal(err)
	}
	check, err := EnsureStorageIdentity(context.Background(), root, identities, true, bindTime)
	if err != nil {
		t.Fatal(err)
	}
	if check.Binding != StorageRebound || check.StorageID != storageIDB || check.PreviousID != storageIDA {
		t.Fatalf("override check = %#v", check)
	}
	if boundID(t, identities) != storageIDB || markerContent(t, root) != storageIDB {
		t.Fatal("override did not bind the database to the mounted marker")
	}
}

func TestStorageIdentityOverrideMarksEmptyReplacementVolume(t *testing.T) {
	root := t.TempDir()
	identities := NewMemory()
	if err := identities.RebindStorageID(context.Background(), storageIDA, bindTime); err != nil {
		t.Fatal(err)
	}
	check, err := EnsureStorageIdentity(context.Background(), root, identities, true, bindTime)
	if err != nil {
		t.Fatal(err)
	}
	if check.Binding != StorageRebound || check.PreviousID != storageIDA || check.StorageID == storageIDA ||
		markerContent(t, root) != check.StorageID || boundID(t, identities) != check.StorageID {
		t.Fatalf("override on empty volume = %#v, marker %q, database %q", check, markerContent(t, root), boundID(t, identities))
	}
}

func TestStorageIdentityRejectsMalformedMarkerWithoutReplacingIt(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "not-a-storage-id")
	for _, rebind := range []bool{false, true} {
		if _, err := EnsureStorageIdentity(context.Background(), root, NewMemory(), rebind, bindTime); err == nil {
			t.Fatalf("malformed marker accepted with rebind=%v", rebind)
		}
	}
	content, err := os.ReadFile(filepath.Join(root, StorageMarkerName))
	if err != nil || string(content) != "not-a-storage-id\n" {
		t.Fatalf("malformed marker was changed: %q, %v", content, err)
	}
}
