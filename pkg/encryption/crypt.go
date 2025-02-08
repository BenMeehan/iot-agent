package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// EncryptionManager defines the interface for encryption and decryption operations.
type EncryptionManagerInterface interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// EncryptionManager implements the EncryptionManagerInterface using AES-GCM.
type EncryptionManager struct {
	key []byte
}

// NewEncryptionManager creates a new AESGCM instance with the provided key.
func NewEncryptionManager(key []byte) (*EncryptionManager, error) {
	const keySize = 32 // AES-256
	if len(key) != keySize {
		return nil, fmt.Errorf("invalid AES key size: got %d bytes, want %d bytes", len(key), keySize)
	}
	return &EncryptionManager{key: key}, nil
}

// Encrypt encrypts the plaintext using AES-GCM and returns the ciphertext, including the nonce.
func (a *EncryptionManager) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES-GCM: %w", err)
	}

	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts the ciphertext using AES-GCM.
func (a *EncryptionManager) Decrypt(ciphertext []byte) ([]byte, error) {
	const nonceSize = 12
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short: must include nonce and encrypted data")
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES-GCM: %w", err)
	}

	plaintext, err := aesgcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}
