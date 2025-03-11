package encryption

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

// SignPayload generates an HMAC-SHA256 signature for the given payload using the signing key.
// The signature is appended to the payload for integrity verification.
func (a *EncryptionManager) SignPayload(payload []byte) ([]byte, error) {
	h := hmac.New(sha256.New, a.signingKey)
	if _, err := h.Write(payload); err != nil {
		return nil, fmt.Errorf("failed to sign payload: %v", err)
	}
	signature := h.Sum(nil)
	return append(payload, signature...), nil
}

// VerifyPayloadSignature checks whether the HMAC-SHA256 signature in the signed payload is valid.
// It extracts the original payload, computes the expected signature, and compares it securely.
func (a *EncryptionManager) VerifyPayloadSignature(signedPayload []byte) bool {
	if len(signedPayload) < 32 { // HMAC-SHA256 produces a 32-byte signature
		return false
	}

	// Separate payload from appended signature
	payload := signedPayload[:len(signedPayload)-32]
	signature := signedPayload[len(signedPayload)-32:]

	h := hmac.New(sha256.New, a.signingKey)
	h.Write(payload)
	expected := h.Sum(nil)

	return hmac.Equal(signature, expected)
}
