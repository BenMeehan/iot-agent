package http_utils

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func DownloadFileByPresignedURL(presignedURL string, outputPath string) error {
	// Create an HTTP client
	client := &http.Client{
		Timeout: 0,
	}

	// Make a GET request to the presigned URL
	resp, err := client.Get(presignedURL)
	if err != nil {
		return fmt.Errorf("failed to download file from presigned URL: %v", err)
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file, received status code: %d", resp.StatusCode)
	}

	// Create or check directory
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// Create or open the output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %v", outputPath, err)
	}
	defer outFile.Close()

	// Stream the response body to the output file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file content to %s: %v", outputPath, err)
	}

	return nil
}

func GetAcknowledgementResponse(jwtToken string, url string, postBody string) (int, []byte, error) {
	fmt.Println("CALLED GetAcknowledgementResponse")
	// Create new request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(postBody)))
	if err != nil {
		return 0, []byte{}, err
	}

	// Add JWT token here
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jwtToken))
	req.Header.Set("Content-Type", "application/json")

	// Create client with timeout
	client := &http.Client{
		Timeout: 15 * time.Second, // ⬅️ set timeout here
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return 0, []byte{}, err
	}
	defer resp.Body.Close()

	// Read response
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, []byte{}, err
	}

	return resp.StatusCode, respBodyBytes, nil
}
