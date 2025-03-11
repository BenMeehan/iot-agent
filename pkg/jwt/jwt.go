package jwt

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
)

// JWTManagerInterface defines methods to manage JWT and refresh tokens.
type JWTManagerInterface interface {
	Initialize(secretPath string) error
	LoadJWT() error
	SaveJWT(token string) error
	GetJWT() string
	IsJWTValid() (bool, error)
	ValidateJWT(token string) (*jwt.Token, error)
	SaveRefreshToken(token string) error
	GetRefreshToken() (string, error)
}

// tokenData holds both JWT and refresh token for storage.
type tokenData struct {
	JWTToken     string `json:"jwt_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// JWTManager manages JWT and refresh tokens with file operations.
type JWTManager struct {
	TokenFilePath     string
	Token             string
	RefreshToken      string
	FileOps           file.FileOperations
	EncryptionManager encryption.EncryptionManagerInterface
	Secret            []byte
}

// NewJWTManager initializes a new JWTManager instance with a single file path.
func NewJWTManager(tokenFilePath string, fileOps file.FileOperations, encryptionManager encryption.EncryptionManagerInterface) JWTManagerInterface {
	return &JWTManager{
		TokenFilePath:     tokenFilePath,
		FileOps:           fileOps,
		EncryptionManager: encryptionManager,
	}
}

func (jm *JWTManager) Initialize(secretPath string) error {
	// Load secret key
	secret, err := jm.FileOps.ReadFileRaw(secretPath)
	if err != nil || len(secret) == 0 {
		return errors.New("failed to read or validate secret key")
	}
	jm.Secret = secret

	// Load JWT and refresh token
	if err := jm.LoadJWT(); err != nil {
		return err
	}

	return nil
}

// LoadJWT reads the JWT and refresh token from the single file.
// If the file does not exist or is empty, it initializes both to empty strings.
func (jm *JWTManager) LoadJWT() error {
	data, err := jm.FileOps.ReadFileRaw(jm.TokenFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			jm.Token = ""
			jm.RefreshToken = ""
			return nil
		}
		return err
	}

	if len(data) == 0 {
		jm.Token = ""
		jm.RefreshToken = ""
		return nil
	}

	decryptedData, err := jm.EncryptionManager.Decrypt(data)
	if err != nil {
		return err
	}

	var tokens tokenData
	if err := json.Unmarshal(decryptedData, &tokens); err != nil {
		return errors.New("failed to parse token data: " + err.Error())
	}

	jm.Token = tokens.JWTToken
	jm.RefreshToken = tokens.RefreshToken
	return nil
}

// SaveJWT saves the given JWT token to the file, preserving the refresh token.
func (jm *JWTManager) SaveJWT(token string) error {
	// Validate the token's signature before saving
	_, err := jm.ValidateJWT(token)
	if err != nil {
		return errors.New("invalid JWT signature: " + err.Error())
	}

	// Load current refresh token if not in memory
	if jm.RefreshToken == "" {
		_, err := jm.GetRefreshToken()
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// Create combined token data
	tokens := tokenData{
		JWTToken:     token,
		RefreshToken: jm.RefreshToken,
	}

	encryptedTokens, err := jm.encryptTokenData(tokens)
	if err != nil {
		return err
	}

	if err := jm.FileOps.WriteFileRaw(jm.TokenFilePath, encryptedTokens); err != nil {
		return err
	}

	jm.Token = token
	return nil
}

// GetJWT retrieves the current JWT token only if it is valid.
func (jm *JWTManager) GetJWT() string {
	if jm.Token == "" {
		return ""
	}

	isValid, err := jm.IsJWTValid()
	if err != nil || !isValid {
		return ""
	}

	return jm.Token
}

// SaveRefreshToken saves the given refresh token to the file, preserving the JWT.
func (jm *JWTManager) SaveRefreshToken(token string) error {
	// Load current JWT if not in memory
	if jm.Token == "" {
		err := jm.LoadJWT()
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// Create combined token data
	tokens := tokenData{
		JWTToken:     jm.Token,
		RefreshToken: token,
	}

	encryptedTokens, err := jm.encryptTokenData(tokens)
	if err != nil {
		return err
	}

	if err := jm.FileOps.WriteFileRaw(jm.TokenFilePath, encryptedTokens); err != nil {
		return err
	}

	jm.RefreshToken = token
	return nil
}

// GetRefreshToken retrieves the current refresh token, loading it from the file if necessary.
func (jm *JWTManager) GetRefreshToken() (string, error) {
	if jm.RefreshToken == "" && jm.Token == "" {
		err := jm.LoadJWT() // Load both jwt and refresh token
		if err != nil {
			return "", err
		}
	}
	if jm.RefreshToken == "" {
		return "", nil // No refresh token yet
	}
	return jm.RefreshToken, nil
}

// IsJWTValid checks if the current JWT token is valid, including signature verification.
func (jm *JWTManager) IsJWTValid() (bool, error) {
	if jm.Token == "" {
		return false, nil
	}

	token, err := jm.ValidateJWT(jm.Token)
	if err != nil {
		return false, nil // Invalid tokens are considered expired/invalid, not an error
	}

	// Check expiration
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false, errors.New("invalid JWT claims format")
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return false, errors.New("JWT expiration (exp) claim missing or invalid")
	}

	expiryTime := time.Unix(int64(exp), 0)
	if time.Now().After(expiryTime) {
		return false, nil
	}

	return true, nil
}

// ValidateJWT checks the given JWT token for validity and signature.
func (jm *JWTManager) ValidateJWT(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method: " + token.Header["alg"].(string))
		}
		return jm.Secret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid JWT token")
	}

	return token, nil
}

// encryptTokenData serializes and encrypts the token data for storage.
func (jm *JWTManager) encryptTokenData(tokens tokenData) ([]byte, error) {
	data, err := json.Marshal(tokens)
	if err != nil {
		return nil, errors.New("failed to serialize token data: " + err.Error())
	}

	encryptedData, err := jm.EncryptionManager.Encrypt(data)
	if err != nil {
		return nil, errors.New("failed to encrypt token data: " + err.Error())
	}

	return encryptedData, nil
}
