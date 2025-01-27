package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
)

// JWTManagerInterface defines the methods to manage JWT tokens.
type JWTManagerInterface interface {
	LoadJWT() error              // Loads the JWT token from the file.
	SaveJWT(token string) error  // Saves the JWT token to the file.
	GetJWT() string              // Retrieves the current JWT token.
	IsJWTExpired() (bool, error) // Checks if the current JWT token is expired.
}

// JWTManager manages the JWT token and its file operations.
type JWTManager struct {
	JWTFilePath       string                                // Path to the JWT file.
	Token             string                                // Current JWT token.
	FileOps           file.FileOperations                   // Interface for file operations.
	EncryptionManager encryption.EncryptionManagerInterface // Interface for encryption operations.
}

// NewJWTManager initializes a new JWTManager instance.
func NewJWTManager(filePath string, fileOps file.FileOperations, encryptionManager encryption.EncryptionManagerInterface) JWTManagerInterface {
	return &JWTManager{
		JWTFilePath:       filePath,
		FileOps:           fileOps,
		EncryptionManager: encryptionManager,
	}
}

// LoadJWT reads the JWT token from the file.
// If the file does not exist or is empty, it initializes the Token to an empty string.
func (jm *JWTManager) LoadJWT() error {
	data, err := jm.FileOps.ReadFileRaw(jm.JWTFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			jm.Token = ""
			return nil
		}
		return err
	}

	if len(data) == 0 {
		jm.Token = ""
		return nil
	}

	decryptedToken, err := jm.EncryptionManager.Decrypt(data)
	if err != nil {
		return err
	}

	jm.Token = string(decryptedToken)
	return nil
}

// SaveJWT saves the given JWT token to the file.
func (jm *JWTManager) SaveJWT(token string) error {
	encryptedToken, err := jm.EncryptionManager.Encrypt([]byte(token))
	if err != nil {
		return err
	}

	if err := jm.FileOps.WriteFileRaw(jm.JWTFilePath, encryptedToken); err != nil {
		return err
	}

	jm.Token = token
	return nil
}

// GetJWT retrieves the current JWT token.
func (jm *JWTManager) GetJWT() string {
	return jm.Token
}

// IsJWTExpired checks if the current JWT token is expired.
func (jm *JWTManager) IsJWTExpired() (bool, error) {
	if jm.Token == "" {
		return true, errors.New("JWT token is empty")
	}

	parts := strings.Split(jm.Token, ".")
	if len(parts) != 3 {
		return true, errors.New("invalid JWT token format")
	}

	payload, err := jwtDecodeBase64(parts[1])
	if err != nil {
		return true, errors.New("failed to decode JWT payload: " + err.Error())
	}

	var claims map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return true, errors.New("failed to parse JWT claims: " + err.Error())
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return true, errors.New("JWT expiration (exp) claim missing or invalid")
	}

	expiryTime := time.Unix(int64(exp), 0)
	return time.Now().After(expiryTime), nil
}

// jwtDecodeBase64 decodes a base64 JWT part.
func jwtDecodeBase64(input string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
