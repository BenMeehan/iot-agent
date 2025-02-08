package models

import "encoding/json"

// RegistrationPayload represents the standardized structure for device registration.
type RegistrationPayload struct {
	// ClientID is the unique identifier for the client.
	ClientID string `json:"client_id,omitempty"`

	// DeviceID is the unique identifier for the device.
	DeviceID string `json:"device_id,omitempty"`

	// Name represents the name of the device.
	Name string `json:"device_name,omitempty"`

	// OrgID represents the organization ID to which the device belongs.
	OrgID string `json:"org_id,omitempty"`

	// Metadata contains additional information about the device, in key-value pairs.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// RegistrationResponse represents the response structure from the server after a successful device registration.
type RegistrationResponse struct {
	// DeviceID is the unique identifier assigned to the device upon successful registration.
	DeviceID string `json:"device_id"`

	// JWTToken is the JSON Web Token provided for the device to authenticate future requests.
	JWTToken string `json:"jwt_token"`
}
