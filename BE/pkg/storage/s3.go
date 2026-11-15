package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	// HTTPClient, when set, carries every request; callers use it to
	// restrict where a user-configured endpoint may connect.
	HTTPClient aws.HTTPClient
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

	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(creds),
	}
	if cfg.HTTPClient != nil {
		options = append(options, awsconfig.WithHTTPClient(cfg.HTTPClient))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
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

// PutWithMetadata stores the content read from r under key with user
// metadata (sent as x-amz-meta-* headers). Pass a seekable reader such as an
// *os.File so the request can be signed over plain HTTP endpoints too.
func (b *S3Backend) PutWithMetadata(ctx context.Context, key string, r io.Reader, contentType string, metadata map[string]string) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.cfg.Bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
		Metadata:    metadata,
	})
	if err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	return nil
}

// s3MaxParts is the most parts one multipart upload may have.
const s3MaxParts = 10000

// PutFileMultipart uploads size bytes of r as key in parts of partSize bytes
// (the last part may be smaller), for objects too large for one PutObject.
// Parts are sent one at a time, each from its own section of r, so a retried
// request rereads the same bytes. On any failure the upload is aborted so no
// parts are left behind, and the original error is returned.
func (b *S3Backend) PutFileMultipart(ctx context.Context, key string, r io.ReaderAt, size, partSize int64, contentType string, metadata map[string]string) (err error) {
	if partSize <= 0 || size < 0 {
		return fmt.Errorf("s3 multipart: invalid size %d or part size %d", size, partSize)
	}
	if (size+partSize-1)/partSize > s3MaxParts {
		return fmt.Errorf("s3 multipart: %d bytes need more than %d parts of %d bytes", size, s3MaxParts, partSize)
	}
	created, err := b.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:            aws.String(b.cfg.Bucket),
		Key:               aws.String(key),
		ContentType:       aws.String(contentType),
		Metadata:          metadata,
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
	})
	if err != nil {
		return fmt.Errorf("s3 multipart create: %w", err)
	}
	uploadID := created.UploadId
	defer func() {
		if err == nil {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = b.client.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
			Bucket: aws.String(b.cfg.Bucket), Key: aws.String(key), UploadId: uploadID,
		})
	}()

	count := max(1, (size+partSize-1)/partSize)
	parts := make([]types.CompletedPart, 0, count)
	for i := range count {
		number, offset := int32(i+1), i*partSize
		length := min(partSize, size-offset)
		out, partErr := b.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:            aws.String(b.cfg.Bucket),
			Key:               aws.String(key),
			UploadId:          uploadID,
			PartNumber:        aws.Int32(number),
			Body:              io.NewSectionReader(r, offset, length),
			ContentLength:     aws.Int64(length),
			ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
		})
		if partErr != nil {
			return fmt.Errorf("s3 multipart part %d: %w", number, partErr)
		}
		parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(number), ChecksumCRC32: out.ChecksumCRC32})
	}
	if _, err = b.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(b.cfg.Bucket),
		Key:             aws.String(key),
		UploadId:        uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		return fmt.Errorf("s3 multipart complete: %w", err)
	}
	return nil
}

// List returns every object whose key starts with prefix, following
// pagination. It implements Lister.
func (b *S3Backend) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.cfg.Bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}
		for _, item := range page.Contents {
			objects = append(objects, Object{
				Key:  aws.ToString(item.Key),
				Size: aws.ToInt64(item.Size),
				ETag: strings.Trim(aws.ToString(item.ETag), `"`),
			})
		}
	}
	return objects, nil
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
