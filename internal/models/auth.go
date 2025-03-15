package models

// AuthResponse represents the expected structure of the authentication response.
type AuthResponse struct {
	JWT          string `json:"jwt"`
	RefreshToken string `json:"refresh_token"`
}

// WrappedPayload represents the final structure sent over MQTT.
type WrappedPayload struct {
	JWT     string      `json:"jwt"`
	Payload interface{} `json:"payload"`
}
