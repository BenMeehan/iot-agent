package s3

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Object Storage interface with new methods for operations
type ObjectStorageClient interface {
	Connect(string, string, string, bool) error
	UploadFile(string, string, multipart.File, int64, string) (error, string, string, string)
	DownloadFileByPresignedURL(string, string) error
}

// Database holds the object storage client instance and logger
type ObjectStorage struct {
	Conn *minio.Client
}

// NewStorage initialization
func NewObjectStorage() ObjectStorageClient {
	return &ObjectStorage{}
}

// Connect establishes the object storage connection using client
func (o *ObjectStorage) Connect(endpoint string, accessKeyID string, secretAccessKey string, useSSL bool) error {
	var err error
	o.Conn, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("Failed to create minio client, %v", err)
	}

	// Check connection by listing buckets
	ctx := context.Background()
	_, err = o.Conn.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("Failed to establish minio connection, %v", err)
	}

	return nil
}

func (o *ObjectStorage) UploadFile(bucketName string, objectName string, fileContent multipart.File, fileSize int64, contentType string) (error, string, string, string) {

	// Check or create bucket
	var err error
	location := "us-east-1"

	ctx := context.Background()
	err = o.Conn.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{
		Region: location,
	})

	if err != nil {
		exists, errBucketExists := o.Conn.BucketExists(ctx, bucketName)
		if !(errBucketExists == nil && exists) {
			return fmt.Errorf("Failed to create bucket. %v", err), "", "", ""
		}
	}

	//ctx := context.Background()
	expiry := time.Hour * 24 * 7

	// Overwrites if same filename already exists
	presignedUrl, err := o.Conn.PresignedGetObject(ctx, bucketName, objectName, expiry, nil)
	if err != nil {
		return err, "", "", ""
	}

	info, err := o.Conn.PutObject(ctx, bucketName, objectName, fileContent, fileSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return err, "", "", ""
	}

	return nil, presignedUrl.String(), objectName, strconv.FormatInt(info.Size, 10)

}

func (o *ObjectStorage) DownloadFileByPresignedURL(presignedURL string, outputPath string) error {
	// Create an HTTP client
	client := &http.Client{
		Timeout: 0,
	}

	// Make a GET request to the presigned URL
	resp, err := client.Get(presignedURL)
	if err != nil {
		return fmt.Errorf("Failed to download file fron presigned URL: %v", err)
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Failed to download file, received status code: %d", resp.StatusCode)
	}

	// Create or check directory
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("Failed to create directory %s: %v", dir, err)
	}

	// Create or open the output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("Failed to create output file %s: %v", outputPath, err)
	}
	defer outFile.Close()

	// Stream the response body to the output file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to write file content to %s: %v", outputPath, err)
	}

	return nil
}
