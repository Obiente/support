package cryptobox

import (
	"encoding/base64"
	"testing"
)

func TestSealBindsCiphertextToContext(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	box, err := NewFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Seal([]byte("private report"), []byte("report-a"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := box.Open(ciphertext, []byte("report-a"))
	if err != nil || string(plaintext) != "private report" {
		t.Fatalf("open = %q, %v", plaintext, err)
	}
	if _, err := box.Open(ciphertext, []byte("report-b")); err == nil {
		t.Fatal("ciphertext opened under a different report context")
	}
}
