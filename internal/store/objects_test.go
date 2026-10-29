package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyObjectRootRemovesItsProbe(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.enc")
	if err := os.WriteFile(existing, []byte("ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyObjectRoot(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "existing.enc" {
		t.Fatalf("object root entries after probe = %v, want only the existing object", entries)
	}
}

func TestVerifyObjectRootRejectsUnavailableRoots(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, root := range map[string]string{
		"missing":      filepath.Join(parent, "missing"),
		"regular file": file,
		"below a file": filepath.Join(file, "private"),
	} {
		if err := VerifyObjectRoot(root); err == nil {
			t.Fatalf("%s object root was accepted", name)
		}
	}
}

func TestVerifyObjectRootRejectsReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits do not prevent file creation on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission bits")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := VerifyObjectRoot(root); err == nil {
		t.Fatal("read-only object root was accepted")
	}
}

func TestFileObjectKeysIgnoreMarkerAndTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	objects, err := NewFileObjects(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	objectKey := strings.Repeat("c", 64) + ".enc"
	for _, name := range []string{objectKey, StorageMarkerName, ".upload-123.tmp", ".storage-id-123.tmp", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, strings.Repeat("d", 64)+".enc"), 0o700); err != nil {
		t.Fatal(err)
	}
	keys, err := objects.Keys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != objectKey {
		t.Fatalf("object keys = %v, want only %s", keys, objectKey)
	}
}
