package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

const magic = "OOSENC1"

func Encrypt(src, dst, password string) error {
	plain, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("crypt: lesen: %w", err)
	}

	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, plain, nil)
	out := append([]byte(magic), ciphertext...)
	return os.WriteFile(dst, out, 0600)
}

func Decrypt(src, password string) ([]byte, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf("crypt: lesen: %w", err)
	}

	if len(data) < len(magic) || string(data[:len(magic)]) != magic {
		return nil, fmt.Errorf("crypt: keine verschlüsselte OOS-Config")
	}
	data = data[len(magic):]

	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("crypt: ungültige Datei")
	}

	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypt: falsches Passwort oder beschädigte Datei")
	}
	return plain, nil
}

func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(magic))
	n, _ := f.Read(buf)
	return n == len(magic) && string(buf) == magic
}

func deriveKey(password string) []byte {
	h := sha256.Sum256([]byte(password))
	return h[:]
}
