package location

import (
	"context"
	"time"

	"googlemaps.github.io/maps"
)

// GoogleGeolocationProvider uses the Google Maps API to get location data.
type GoogleGeolocationProvider struct {
	client *maps.Client // Maps API client for making geolocation requests
}

// NewGoogleGeolocationProvider creates a new GoogleGeolocationProvider instance.
func NewGoogleGeolocationProvider(apiKey string) (*GoogleGeolocationProvider, error) {
	c, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}

	return &GoogleGeolocationProvider{
		client: c,
	}, nil
}

// GetLocation retrieves the device's location using Google Maps Geolocation API.
func (g *GoogleGeolocationProvider) GetLocation() (Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wifiAPs, err := getWiFiAccessPoints()
	if err != nil {
		return Location{}, err // Return error if WiFi access points retrieval fails
	}

	cellTowers, err := getCellTowers()
	if err != nil {
		return Location{}, err // Return error if cell towers retrieval fails
	}

	// Prepare the geolocation request with available data
	req := &maps.GeolocationRequest{
		ConsiderIP:       true,
		WiFiAccessPoints: wifiAPs,
		CellTowers:       cellTowers,
	}

	resp, err := g.client.Geolocate(ctx, req) // Send the geolocation request
	if err != nil {
		return Location{}, err
	}

	// Return the location data obtained from the response
	return Location{
		Latitude:  resp.Location.Lat,
		Longitude: resp.Location.Lng,
		Accuracy:  resp.Accuracy,
	}, nil
}
