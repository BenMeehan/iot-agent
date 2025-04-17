package location

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/adrianmo/go-nmea"
	"github.com/tarm/serial"
)

// DeviceSensorProvider retrieves location data from a serial GPS device.
type DeviceSensorProvider struct {
	port       string
	baudRate   int
	timeout    time.Duration
	serialPort *serial.Port
	scanner    *bufio.Scanner
	lastLoc    *Location
	mutex      sync.Mutex
}

// NewDeviceSensorProvider initializes a new GPS provider for the given serial port and baud rate.
func NewDeviceSensorProvider(port string, baudRate int, timeout time.Duration) (*DeviceSensorProvider, error) {
	if port == "" {
		return nil, errors.New("serial port cannot be empty")
	}
	if baudRate <= 0 {
		return nil, errors.New("baud rate must be positive")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}

	// Initialize serial port
	config := &serial.Config{
		Name:        port,
		Baud:        baudRate,
		ReadTimeout: timeout / 2, // Set read timeout to half the total timeout for responsiveness
	}
	serialPort, err := serial.OpenPort(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s: %w", port, err)
	}

	provider := &DeviceSensorProvider{
		port:       port,
		baudRate:   baudRate,
		timeout:    timeout,
		serialPort: serialPort,
		scanner:    bufio.NewScanner(serialPort),
	}

	return provider, nil
}

// GetLocation retrieves a valid GPS location from the serial device.
func (d *DeviceSensorProvider) GetLocation() (Location, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	// Reset scanner buffer to avoid stale data
	d.scanner = bufio.NewScanner(d.serialPort)

	for d.scanner.Scan() {
		select {
		case <-ctx.Done():
			if d.lastLoc != nil {
				return *d.lastLoc, nil
			}
			return Location{}, errors.New("no valid GPS data found within timeout")
		default:
			line := d.scanner.Text()
			if strings.HasPrefix(line, "$GPGGA") {
				sentence, err := nmea.Parse(line)
				if err != nil {
					continue
				}

				if gga, ok := sentence.(nmea.GGA); ok {
					if gga.FixQuality == nmea.Invalid {
						continue
					}

					// Convert HDOP to approximate accuracy in meters (assuming 5m base accuracy)
					accuracy := float64(gga.HDOP) * 5.0

					loc := Location{
						Latitude:  gga.Latitude,
						Longitude: gga.Longitude,
						Accuracy:  accuracy,
					}
					d.lastLoc = &loc
					return loc, nil
				}
			}
		}
	}

	if err := d.scanner.Err(); err != nil {
		return Location{}, fmt.Errorf("serial read error on port %s: %w", d.port, err)
	}

	if d.lastLoc != nil {
		return *d.lastLoc, nil
	}
	return Location{}, errors.New("no valid GPS data found")
}

// Close shuts down the serial port connection.
func (d *DeviceSensorProvider) Close() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.serialPort == nil {
		return nil
	}

	err := d.serialPort.Close()
	if err != nil {
		return fmt.Errorf("failed to close serial port %s: %w", d.port, err)
	}

	d.serialPort = nil
	d.scanner = nil
	return nil
}
