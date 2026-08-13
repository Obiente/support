package cryptobox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const keyBytes = 32

type Box struct {
	aead cipher.AEAD
}

func NewFromBase64(encoded string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode SUPPORT_DATA_KEY: %w", err)
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf("SUPPORT_DATA_KEY must decode to %d bytes", keyBytes)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (box *Box) Seal(plaintext, context []byte) ([]byte, error) {
	nonce := make([]byte, box.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return box.aead.Seal(nonce, nonce, plaintext, context), nil
}

func (box *Box) Open(ciphertext, context []byte) ([]byte, error) {
	if len(ciphertext) < box.aead.NonceSize() {
		return nil, errors.New("ciphertext is truncated")
	}
	nonce := ciphertext[:box.aead.NonceSize()]
	return box.aead.Open(nil, nonce, ciphertext[box.aead.NonceSize():], context)
}
