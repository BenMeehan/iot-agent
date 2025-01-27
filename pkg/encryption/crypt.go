package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// EncryptionManagerInterface defines the methods for managing encryption and decryption.
type EncryptionManagerInterface interface {
	Encrypt(data []byte) ([]byte, error) // Encrypts the given data using AES-GCM.
	Decrypt(data []byte) ([]byte, error) // Decrypts the given data using AES-GCM.
	SetKey(key []byte) error             // Sets the AES encryption key.
	GetKey() []byte                      // Retrieves the current AES encryption key.
}

// EncryptionManager manages encryption and decryption operations using AES-GCM.
type EncryptionManager struct {
	AESKey []byte // AES encryption key.
}

// NewEncryptionManager initializes a new EncryptionManager instance with the given key.
func NewEncryptionManager(key []byte) (*EncryptionManager, error) {
	if len(key) != 32 { // AES-256 requires a 32-byte key.
		return nil, fmt.Errorf("invalid AES key size: got %d bytes, want 32 bytes", len(key))
	}
	return &EncryptionManager{AESKey: key}, nil
}

// SetKey sets the AES encryption key.
func (em *EncryptionManager) SetKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("invalid AES key size: got %d bytes, want 32 bytes", len(key))
	}
	em.AESKey = key
	return nil
}

// GetKey retrieves the current AES encryption key.
func (em *EncryptionManager) GetKey() []byte {
	return em.AESKey
}

// Encrypt encrypts plaintext using AES-GCM and returns the ciphertext, including the nonce.
func (em *EncryptionManager) Encrypt(plaintext []byte) ([]byte, error) {
	// Create a new AES cipher block.
	block, err := aes.NewCipher(em.AESKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	// Generate a nonce (12 bytes for AES-GCM).
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Create AES-GCM cipher instance.
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES-GCM: %w", err)
	}

	// Encrypt the plaintext and append the nonce to the ciphertext.
	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// Decrypt decrypts the encrypted payload using AES-GCM.
func (em *EncryptionManager) Decrypt(ciphertext []byte) ([]byte, error) {
	// Validate ciphertext length (at least nonce size + encrypted data).
	const nonceSize = 12
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short: must include nonce and encrypted data")
	}

	// Split the nonce and encrypted data.
	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	// Create a new AES cipher block.
	block, err := aes.NewCipher(em.AESKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	// Create GCM (Galois/Counter Mode) for the block.
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES-GCM: %w", err)
	}

	// Decrypt the data.
	plaintext, err := aesGCM.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}
