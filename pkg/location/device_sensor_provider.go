package location

import (
	"bufio"
	"errors"
	"strings"

	"github.com/adrianmo/go-nmea"
	"github.com/tarm/serial"
)

// DeviceSensorProvider is responsible for retrieving location data from a GPS device connected via serial port.
type DeviceSensorProvider struct {
	port     string // Serial port to which the GPS device is connected
	baudRate int    // Baud rate for the serial communication
}

// NewDeviceSensorProvider creates a new instance of DeviceSensorProvider with the specified port and baud rate.
func NewDeviceSensorProvider(port string, baudRate int) *DeviceSensorProvider {
	return &DeviceSensorProvider{
		port:     port,
		baudRate: baudRate,
	}
}

// GetLocation reads GPS data from the device and returns the device's location.
func (d *DeviceSensorProvider) GetLocation() (Location, error) {
	c := &serial.Config{Name: d.port, Baud: d.baudRate}
	s, err := serial.OpenPort(c)
	if err != nil {
		return Location{}, err
	}
	defer s.Close() // Ensure the port is closed when done

	// Create a scanner to read lines from the serial port
	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		line := scanner.Text()                 // Read a line from the GPS output
		if strings.HasPrefix(line, "$GPGGA") { // Check for GGA sentences
			sentence, err := nmea.Parse(line) // Parse the NMEA sentence
			if err != nil {
				return Location{}, err
			}

			if gga, ok := sentence.(nmea.GGA); ok { // Check if the sentence is a GGA
				// Return the location data from the GGA sentence
				return Location{
					Latitude:  gga.Latitude,
					Longitude: gga.Longitude,
					Accuracy:  float64(gga.HDOP), // Use HDOP as a proxy for accuracy
				}, nil
			}
		}
	}

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return Location{}, err
	}

	return Location{}, errors.New("no valid GPS data found")
}
