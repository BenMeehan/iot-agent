package models

// AuthRequest represents a message sent by the device to the AuthService
// to initiate the authentication process.
type AuthRequest struct {
	ClientID     string `json:"client_id,omitempty"`     // Unique identifier for the client
	DeviceID     string `json:"device_id,omitempty"`     // Unique identifier for the device
	Key          []byte `json:"certificate,omitempty"`   // Certificate or key for authentication
	RefreshToken string `json:"refresh_token,omitempty"` // Token used to refresh authentication
}

// AuthResponse represents the expected structure of the authentication response.
type AuthResponse struct {
	AccessToken  string `json:"access_token"`  // JWT access token for authentication
	RefreshToken string `json:"refresh_token"` // Refresh token for renewing authentication
	ExpiresIn    int    `json:"expires_in"`    // Expiration time of the access token in seconds
}

// WrappedPayload represents the final structure sent over MQTT.
type WrappedPayload struct {
	JWT     string      `json:"jwt"`     // JWT token used for secure communication
	Payload interface{} `json:"payload"` // Actual data payload being transmitted
}
