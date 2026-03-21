package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds the configuration for an S3-compatible backend.
type S3Config struct {
	Endpoint   string
	Bucket     string
	Region     string
	AccessKey  string
	SecretKey  string
	Public     bool
	PresignTTL time.Duration
	CDNBaseURL string
}

// S3Backend stores files in an S3-compatible object store.
type S3Backend struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	cfg           S3Config
}

func NewS3Backend(cfg S3Config) (*S3Backend, error) {
	if cfg.PresignTTL == 0 {
		cfg.PresignTTL = 1 * time.Hour
	}

	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = true // required for SeaweedFS, MinIO, etc.
	})

	return &S3Backend{
		client:        client,
		presignClient: s3.NewPresignClient(client),
		cfg:           cfg,
	}, nil
}

func (b *S3Backend) Put(ctx context.Context, key string, r io.Reader, contentType string) (int64, error) {
	cr := &countingReader{r: r}
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.cfg.Bucket),
		Key:         aws.String(key),
		Body:        cr,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 put: %w", err)
	}
	return cr.n, nil
}

func (b *S3Backend) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get: %w", err)
	}
	return out.Body, nil
}

func (b *S3Backend) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	return err
}

func (b *S3Backend) URL(ctx context.Context, key string) (string, error) {
	if b.cfg.Public {
		if b.cfg.CDNBaseURL != "" {
			return fmt.Sprintf("%s/%s", b.cfg.CDNBaseURL, key), nil
		}
		return fmt.Sprintf("%s/%s/%s", b.cfg.Endpoint, b.cfg.Bucket, key), nil
	}
	req, err := b.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(b.cfg.PresignTTL))
	if err != nil {
		return "", fmt.Errorf("presign: %w", err)
	}
	return req.URL, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
