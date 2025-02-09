package location

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"

	"googlemaps.github.io/maps"
)

func getWiFiAccessPoints() ([]maps.WiFiAccessPoint, error) {
	cmd := exec.Command("nmcli", "-t", "-f", "BSSID,SIGNAL", "dev", "wifi", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var wifiAPs []maps.WiFiAccessPoint
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 2 {
			continue
		}
		macAddress := parts[0]
		signalStrength, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}
		wifiAPs = append(wifiAPs, maps.WiFiAccessPoint{
			MACAddress:     macAddress,
			SignalStrength: signalStrength,
		})
	}

	return wifiAPs, nil
}

func getCellTowers() ([]maps.CellTower, error) {
	cmd := exec.Command("mmcli", "-m", "0", "--output-keyvalue")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
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
			mcc, _ := strconv.Atoi(value)
			cellTower.MobileCountryCode = mcc
		case "modem.3gpp.mnc":
			mnc, _ := strconv.Atoi(value)
			cellTower.MobileNetworkCode = mnc
		case "modem.3gpp.lac":
			lac, _ := strconv.ParseInt(value, 16, 32)
			cellTower.LocationAreaCode = int(lac)
		case "modem.3gpp.cid":
			cid, _ := strconv.ParseInt(value, 16, 32)
			cellTower.CellID = int(cid)
		}
	}

	return []maps.CellTower{cellTower}, nil
}
