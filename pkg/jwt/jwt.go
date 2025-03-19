package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
)

// TokenData represents the structure for storing both tokens.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Claims represents the standard JWT claims we expect in tokens.
type Claims struct {
	Exp float64 `json:"exp"`
	// Add other claims as needed (e.g., "sub", "iss", "aud")
}

// JWTManagerInterface defines the methods to manage JWT tokens.
type JWTManagerInterface interface {
	LoadTokens() error
	SaveTokens(accessToken, refreshToken string, expiresIn int) error
	GetJWT() string
	IsJWTValid() (bool, error)
	GetRefreshToken() (string, int)
	VerifySignature(secret []byte, token string) (bool, error)
	CheckExpiration(token, tokenType string) (bool, error)
}

// JWTManager manages JWT tokens and their file operations.
type JWTManager struct {
	jwtFilePath       string
	secret            []byte
	accessToken       string
	refreshToken      string
	expiresIn         int
	fileOps           file.FileOperations
	encryptionManager encryption.EncryptionManagerInterface
	mutex             sync.Mutex
}

// NewJWTManager initializes a new JWTManager instance and loads the secret key from a file.
func NewJWTManager(filePath, secretFilePath string, fileOps file.FileOperations, encryptionManager encryption.EncryptionManagerInterface) (JWTManagerInterface, error) {
	manager := &JWTManager{
		jwtFilePath:       filePath,
		fileOps:           fileOps,
		encryptionManager: encryptionManager,
		mutex:             sync.Mutex{},
	}

	secret, err := fileOps.ReadFileRaw(secretFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("secret file does not exist: %s", secretFilePath)
		}
		return nil, fmt.Errorf("failed to read secret file: %w", err)
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("secret file is empty: %s", secretFilePath)
	}
	manager.secret = secret

	return manager, nil
}

// LoadTokens reads both tokens from the file and sets them if valid.
func (m *JWTManager) LoadTokens() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	data, err := m.fileOps.ReadFileRaw(m.jwtFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			m.accessToken = ""
			m.refreshToken = ""
			m.expiresIn = 0
			return nil
		}
		return fmt.Errorf("failed to read token file: %w", err)
	}

	if len(data) == 0 {
		m.accessToken = ""
		m.refreshToken = ""
		m.expiresIn = 0
		return nil
	}

	decryptedData, err := m.encryptionManager.Decrypt(data)
	if err != nil {
		return fmt.Errorf("failed to decrypt token data: %w", err)
	}

	var tokenData TokenData
	if err := json.Unmarshal(decryptedData, &tokenData); err != nil {
		return fmt.Errorf("failed to parse token data: %w", err)
	}

	m.accessToken = tokenData.AccessToken
	m.refreshToken = tokenData.RefreshToken
	return nil
}

// saveToFile encrypts and writes token data to the file.
func (m *JWTManager) saveToFile(tokenData TokenData) error {
	jsonData, err := json.Marshal(tokenData)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	encryptedData, err := m.encryptionManager.Encrypt(jsonData)
	if err != nil {
		return fmt.Errorf("failed to encrypt token data: %w", err)
	}

	if err := m.fileOps.WriteFileRaw(m.jwtFilePath, encryptedData); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}
	return nil
}

// SaveTokens saves both access and refresh tokens after validation.
func (m *JWTManager) SaveTokens(accessToken, refreshToken string, expiresIn int) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Validate access token if present
	if accessToken != "" {
		valid, err := m.validateToken(accessToken, constants.AccessToken)
		if err != nil {
			return fmt.Errorf("access token validation failed: %w", err)
		}
		if !valid {
			return errors.New("access token is invalid or expired")
		}
	}

	m.accessToken = accessToken
	m.refreshToken = refreshToken
	m.expiresIn = expiresIn
	return m.saveToFile(TokenData{
		AccessToken:  m.accessToken,
		RefreshToken: m.refreshToken,
		ExpiresIn:    m.expiresIn,
	})
}

// GetJWT retrieves the current access token
func (m *JWTManager) GetJWT() string {
	return m.accessToken
}

// GetRefreshToken retrieves the current refresh token
func (m *JWTManager) GetRefreshToken() (string, int) {
	return m.refreshToken, m.expiresIn
}

// jwtDecodeBase64 decodes a base64-encoded JWT part.
func jwtDecodeBase64(input string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// VerifySignature verifies the HMAC signature of a given token using the provided secret.
func (m *JWTManager) VerifySignature(secret []byte, token string) (bool, error) {
	if token == "" {
		return false, errors.New("no token provided")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, errors.New("invalid token format")
	}

	message := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(message))
	return hmac.Equal(signature, h.Sum(nil)), nil
}

// validateToken checks if a JWT token is valid (signature and expiration).
func (m *JWTManager) validateToken(token, tokenType string) (bool, error) {
	if token == "" {
		return false, nil
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid %s token format", tokenType)
	}

	// Verify signature
	message := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false, fmt.Errorf("failed to decode %s token signature: %w", tokenType, err)
	}

	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(message))
	if !hmac.Equal(signature, h.Sum(nil)) {
		return false, fmt.Errorf("invalid %s token signature", tokenType)
	}

	// Check expiration
	payload, err := jwtDecodeBase64(parts[1])
	if err != nil {
		return false, fmt.Errorf("failed to decode %s token payload: %w", tokenType, err)
	}

	var claims Claims
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return false, fmt.Errorf("failed to parse %s token claims: %w", tokenType, err)
	}

	if claims.Exp == 0 {
		return false, fmt.Errorf("%s token expiration (exp) claim missing", tokenType)
	}

	expiryTime := time.Unix(int64(claims.Exp), 0)
	return !time.Now().After(expiryTime), nil
}

// checkExpiration checks only the expiration of a token.
func (m *JWTManager) CheckExpiration(token, tokenType string) (bool, error) {
	if token == "" {
		return false, nil
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid %s token format", tokenType)
	}

	payload, err := jwtDecodeBase64(parts[1])
	if err != nil {
		return false, fmt.Errorf("failed to decode %s token payload: %w", tokenType, err)
	}

	var claims Claims
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return false, fmt.Errorf("failed to parse %s token claims: %w", tokenType, err)
	}

	if claims.Exp == 0 {
		return false, fmt.Errorf("%s token expiration (exp) claim missing", tokenType)
	}

	expiryTime := time.Unix(int64(claims.Exp), 0)
	return !time.Now().After(expiryTime), nil
}

// IsJWTValid checks if the current access token is valid (signature and expiration).
func (m *JWTManager) IsJWTValid() (bool, error) {
	return m.validateToken(m.accessToken, constants.AccessToken)
}
