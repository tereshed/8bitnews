package youtube

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// Sealer encrypts a channel's refresh token at rest: the database and its
// backups hold only what this produces, and the key lives elsewhere.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer takes the key as 64 hex digits (HUB_YOUTUBE_TOKEN_KEY).
func NewSealer(hexKey string) (*Sealer, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("the token key must be 64 hex digits")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plain, with the channel id bound in: a token copied to another
// channel's row won't open.
func (s *Sealer) Seal(plain, channelID string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), []byte(channelID))), nil
}

// Open reads back what Seal made.
func (s *Sealer) Open(sealed, channelID string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", errors.New("not a sealed token")
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte(channelID))
	if err != nil {
		return "", fmt.Errorf("opening the token of %s: %w", channelID, err)
	}
	return string(plain), nil
}
