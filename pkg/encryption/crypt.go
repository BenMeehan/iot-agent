package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/benmeehan/iot-agent/pkg/file"
)

// EncryptionManagerInterface defines encryption and decryption methods.
type EncryptionManagerInterface interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// EncryptionManager implements AES-GCM encryption.
type EncryptionManager struct {
	key        []byte
	fileClient file.FileOperations
	aesgcm     cipher.AEAD
}

// NewEncryptionManager creates a new EncryptionManager instance.
func NewEncryptionManager(fileClient file.FileOperations) *EncryptionManager {
	return &EncryptionManager{fileClient: fileClient}
}

// Initialize loads and caches the AES key and cipher.
func (a *EncryptionManager) Initialize(AESKeyPath string) error {
	key, err := a.fileClient.ReadFileRaw(AESKeyPath)
	if err != nil || len(key) == 0 {
		return fmt.Errorf("failed to read or validate AES key: %w", err)
	}
	const keySize = 32
	if len(key) != keySize {
		return fmt.Errorf("invalid AES key size: got %d bytes, want %d bytes", len(key), keySize)
	}
	a.key = key

	// Cache AES-GCM object
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	a.aesgcm, err = cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create AES-GCM: %w", err)
	}

	return nil
}

// Encrypt encrypts plaintext using AES-GCM.
func (a *EncryptionManager) Encrypt(plaintext []byte) ([]byte, error) {
	if a.aesgcm == nil {
		return nil, errors.New("encryption manager not initialized")
	}

	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := a.aesgcm.Seal(nonce[:], nonce[:], plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext using AES-GCM.
func (a *EncryptionManager) Decrypt(ciphertext []byte) ([]byte, error) {
	const nonceSize = 12
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short: must include nonce and encrypted data")
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	if a.aesgcm == nil {
		return nil, errors.New("encryption manager not initialized")
	}

	plaintext, err := a.aesgcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}
