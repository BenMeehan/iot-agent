package location

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"googlemaps.github.io/maps"
)

// GoogleGeolocationProvider uses the Google Maps API to retrieve location data.
type GoogleGeolocationProvider struct {
	client *maps.Client // Maps API client for geolocation requests
	mutex  sync.Mutex   // Ensures thread-safe access to client
}

// NewGoogleGeolocationProvider initializes a new GoogleGeolocationProvider with the given API key.
func NewGoogleGeolocationProvider(apiKey string) (*GoogleGeolocationProvider, error) {
	if apiKey == "" {
		return nil, errors.New("API key cannot be empty")
	}

	client, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Google Maps client: %w", err)
	}

	return &GoogleGeolocationProvider{
		client: client,
	}, nil
}

// GetLocation retrieves the device's location using the Google Maps Geolocation API.
func (g *GoogleGeolocationProvider) GetLocation() (Location, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wifiAPs, err := getWiFiAccessPoints(ctx)
	if err != nil {
		return Location{}, fmt.Errorf("failed to get WiFi access points: %w", err)
	}

	cellTowers, err := getCellTowers(ctx, 0)
	if err != nil {
		return Location{}, fmt.Errorf("failed to get cell towers: %w", err)
	}

	req := &maps.GeolocationRequest{
		ConsiderIP:       true,
		WiFiAccessPoints: wifiAPs,
		CellTowers:       cellTowers,
	}

	resp, err := g.client.Geolocate(ctx, req)
	if err != nil {
		return Location{}, fmt.Errorf("failed to geolocate: %w", err)
	}

	return Location{
		Latitude:  resp.Location.Lat,
		Longitude: resp.Location.Lng,
		Accuracy:  resp.Accuracy,
	}, nil
}

// Stop releases any resources held by the provider.
func (g *GoogleGeolocationProvider) Close() error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.client = nil
	return nil
}
