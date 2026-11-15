package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/kuayle/kuayle-backend/pkg/validate"
)

// BackupS3Destination is the external bucket backups are copied to.
type BackupS3Destination struct {
	Endpoint  string
	Bucket    string
	Region    string
	Prefix    string
	AccessKey string
	SecretKey string
}

// Key returns the object key for a file name under the destination prefix.
func (d BackupS3Destination) Key(name string) string {
	prefix := strings.Trim(d.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// Label is the destination shown in the history, without credentials.
func (d BackupS3Destination) Label() string {
	label := "s3://" + d.Bucket
	if prefix := strings.Trim(d.Prefix, "/"); prefix != "" {
		label += "/" + prefix
	}
	return label
}

// backupObjectStore is the bucket operations a backup needs.
type backupObjectStore interface {
	PutFile(ctx context.Context, key, localPath, checksum string) error
	PutBytes(ctx context.Context, key string, body []byte) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// newBackupObjectStore opens the destination with the S3 client from
// pkg/storage. The endpoint is sysadmin-configured, so unless private
// networks are allowed every connection is dialled through
// validate.PublicAddressControl, and redirects are never followed.
func newBackupObjectStore(dest BackupS3Destination, allowPrivateNetwork bool) (backupObjectStore, error) {
	backend, err := storage.NewS3Backend(storage.S3Config{
		Endpoint:   dest.Endpoint,
		Bucket:     dest.Bucket,
		Region:     dest.Region,
		AccessKey:  dest.AccessKey,
		SecretKey:  dest.SecretKey,
		HTTPClient: backupS3HTTPClient(allowPrivateNetwork),
	})
	if err != nil {
		return nil, err
	}
	return s3BackupStore{backend: backend}, nil
}

func backupS3HTTPClient(allowPrivateNetwork bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivateNetwork {
		dialer.Control = validate.PublicAddressControl
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConnsPerHost:   4,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type s3BackupStore struct {
	backend *storage.S3Backend
}

// Archives above backupMultipartThreshold are uploaded in parts, which lifts
// the 5 GiB single PUT limit. Variables so tests can use small sizes.
var (
	backupMultipartThreshold int64 = 64 << 20
	backupMinPartSize        int64 = 64 << 20
)

// backupPartSize picks a part size that keeps an archive of size bytes
// within 9,000 parts, below S3's limit of 10,000.
func backupPartSize(size int64) int64 {
	return max(backupMinPartSize, (size+8999)/9000)
}

func (s s3BackupStore) PutFile(ctx context.Context, key, localPath, checksum string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	metadata := map[string]string{"sha256": checksum}
	if info.Size() > backupMultipartThreshold {
		return s.backend.PutFileMultipart(ctx, key, file, info.Size(), backupPartSize(info.Size()), "application/octet-stream", metadata)
	}
	return s.backend.PutWithMetadata(ctx, key, file, "application/octet-stream", metadata)
}

func (s s3BackupStore) PutBytes(ctx context.Context, key string, body []byte) error {
	return s.backend.PutWithMetadata(ctx, key, strings.NewReader(string(body)), "text/plain", nil)
}

func (s s3BackupStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.backend.Get(ctx, key)
}

func (s s3BackupStore) Delete(ctx context.Context, key string) error {
	return s.backend.Delete(ctx, key)
}

// validateBackupS3Destination checks the fields a destination needs.
func validateBackupS3Destination(dest BackupS3Destination) error {
	if strings.TrimSpace(dest.Bucket) == "" {
		return backupInvalid("S3 bucket is required")
	}
	if strings.TrimSpace(dest.Region) == "" {
		return backupInvalid("S3 region is required")
	}
	if dest.AccessKey == "" || dest.SecretKey == "" {
		return backupInvalid("S3 access key and secret key are required")
	}
	if dest.Endpoint != "" {
		parsed, err := url.Parse(dest.Endpoint)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return backupInvalid("S3 endpoint must be an http(s) URL without credentials")
		}
	}
	if strings.Contains(dest.Prefix, "..") {
		return backupInvalid("S3 prefix must not contain '..'")
	}
	return nil
}

// testBackupObjectStore writes and deletes a small object under the prefix
// to prove the credentials can write to the bucket.
func testBackupObjectStore(ctx context.Context, store backupObjectStore, dest BackupS3Destination) error {
	key := dest.Key(".kuayle-write-test-" + uuid.NewString())
	if err := store.PutBytes(ctx, key, []byte("kuayle backup write test\n")); err != nil {
		return fmt.Errorf("write test object: %w", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete test object: %w", err)
	}
	return nil
}

// backupObjectName returns the object name of a local backup file.
func backupObjectName(localPath string) string {
	return path.Base(strings.ReplaceAll(localPath, "\\", "/"))
}

var errBackupNoDestination = errors.New("S3 destination is not configured")
