package http_utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
