package location

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"googlemaps.github.io/maps"
)

// getWiFiAccessPoints retrieves nearby WiFi access points using nmcli.
func getWiFiAccessPoints(ctx context.Context) ([]maps.WiFiAccessPoint, error) {
	// Verify nmcli is available
	if _, err := exec.LookPath("nmcli"); err != nil {
		return nil, fmt.Errorf("nmcli not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, "nmcli", "-t", "-f", "BSSID,SIGNAL", "dev", "wifi", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nmcli: %w", err)
	}

	var wifiAPs []maps.WiFiAccessPoint
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 2 {
			continue
		}
		macAddress := strings.TrimSpace(parts[0])
		if !isValidMAC(macAddress) {
			continue
		}
		signal, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		wifiAPs = append(wifiAPs, maps.WiFiAccessPoint{
			MACAddress:     macAddress,
			SignalStrength: float64(signal),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan nmcli output: %w", err)
	}

	return wifiAPs, nil
}

// getCellTowers retrieves nearby cell towers using mmcli for the given modem index.
func getCellTowers(ctx context.Context, modemIndex int) ([]maps.CellTower, error) {
	// Verify mmcli is available
	if _, err := exec.LookPath("mmcli"); err != nil {
		return nil, fmt.Errorf("mmcli not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, "mmcli", "-m", strconv.Itoa(modemIndex), "--output-keyvalue")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run mmcli for modem %d: %w", modemIndex, err)
	}

	var cellTower maps.CellTower
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "modem.3gpp.mcc":
			mcc, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			cellTower.MobileCountryCode = mcc
		case "modem.3gpp.mnc":
			mnc, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			cellTower.MobileNetworkCode = mnc
		case "modem.3gpp.lac":
			lac, err := strconv.ParseInt(value, 16, 32)
			if err != nil {
				continue
			}
			cellTower.LocationAreaCode = int(lac)
		case "modem.3gpp.cid":
			cid, err := strconv.ParseInt(value, 16, 32)
			if err != nil {
				continue
			}
			cellTower.CellID = int(cid)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan mmcli output: %w", err)
	}

	// Validate cell tower data
	if cellTower.MobileCountryCode == 0 || cellTower.MobileNetworkCode == 0 {
		return nil, errors.New("incomplete cell tower data")
	}

	return []maps.CellTower{cellTower}, nil
}

// isValidMAC checks if the MAC address is in a valid format (e.g., "00:14:22:01:23:45").
func isValidMAC(mac string) bool {
	parts := strings.Split(mac, ":")
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		if _, err := strconv.ParseInt(part, 16, 8); err != nil {
			return false
		}
	}
	return true
}
