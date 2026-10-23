// Package secure holds token, hashing and encryption helpers.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Token returns a random URL-safe token with the given prefix, e.g. "wfm_pat_".
func Token(prefix string, bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Hash returns the hex sha256 of a token. Tokens are stored hashed, never in plain text.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func HMACSHA256Hex(secret string, payload []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

// Box encrypts secrets at rest (SCM credentials, webhook secrets) with AES-256-GCM.
type Box struct {
	aead    cipher.AEAD
	signKey []byte
}

func NewBox(secretKey string) (*Box, error) {
	encKey, err := hkdf.Key(sha256.New, []byte(secretKey), []byte("wfm"), "encryption", 32)
	if err != nil {
		return nil, err
	}
	signKey, err := hkdf.Key(sha256.New, []byte(secretKey), []byte("wfm"), "signing", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, signKey: signKey}, nil
}

func (b *Box) Seal(plaintext []byte) string {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b.aead.Seal(nonce, nonce, plaintext, nil))
}

func (b *Box) Open(sealed string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return nil, errors.New("sealed value too short")
	}
	return b.aead.Open(nil, raw[:n], raw[n:], nil)
}

// Sign returns value.signature, for tamper-proof cookies.
func (b *Box) Sign(value string) string {
	m := hmac.New(sha256.New, b.signKey)
	m.Write([]byte(value))
	return value + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Verify returns the value if the signature is valid.
func (b *Box) Verify(signed string) (string, bool) {
	i := strings.LastIndexByte(signed, '.')
	if i <= 0 {
		return "", false
	}
	value := signed[:i]
	return value, Equal(b.Sign(value), signed)
}
