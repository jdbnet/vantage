package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
)

const (
	dekSize   = 32
	saltSize  = 16
	nonceSize = 12
)

var ErrDecrypt = errors.New("decrypt failed")

type Box struct {
	dek []byte
}

func NewDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, err
	}
	return dek, nil
}

func Open(dek []byte) *Box {
	cp := make([]byte, len(dek))
	copy(cp, dek)
	return &Box{dek: cp}
}

func (b *Box) DEK() []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b.dek))
	copy(cp, b.dek)
	return cp
}

func (b *Box) Encrypt(plain string) (string, error) {
	if b == nil || len(b.dek) == 0 {
		return "", errors.New("crypto box is locked")
	}
	block, err := aes.NewCipher(b.dek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (b *Box) Decrypt(blob string) (string, error) {
	if b == nil || len(b.dek) == 0 {
		return "", errors.New("crypto box is locked")
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", ErrDecrypt
	}
	if len(raw) < nonceSize {
		return "", ErrDecrypt
	}
	block, err := aes.NewCipher(b.dek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

func WrapDEK(password string, dek []byte) (string, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := derive(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, dek, nil)
	out := make([]byte, 0, saltSize+nonceSize+len(ct))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func UnwrapDEK(password, wrapped string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, ErrDecrypt
	}
	if len(raw) < saltSize+nonceSize+16 {
		return nil, ErrDecrypt
	}
	salt := raw[:saltSize]
	nonce := raw[saltSize : saltSize+nonceSize]
	ct := raw[saltSize+nonceSize:]
	key := derive(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	dek, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return dek, nil
}

func derive(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, dekSize)
}

func WriteDEKFile(path string, dek []byte) error {
	return os.WriteFile(path, dek, 0o600)
}

func ReadDEKFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != dekSize {
		return nil, fmt.Errorf("invalid dek file")
	}
	return b, nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := derive(password, salt)
	out := append(salt, key...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func VerifyPassword(password, encoded string) bool {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != saltSize+dekSize {
		return false
	}
	salt := raw[:saltSize]
	want := raw[saltSize:]
	got := derive(password, salt)
	if len(got) != len(want) {
		return false
	}
	var v byte
	for i := range got {
		v |= got[i] ^ want[i]
	}
	return v == 0
}
