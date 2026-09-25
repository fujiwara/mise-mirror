package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Storage is a destination of mirrored files.
type Storage interface {
	// Exists reports whether the object for key exists.
	Exists(ctx context.Context, key string) (bool, error)
	// Put stores the content of src as key.
	Put(ctx context.Context, key string, src *os.File) error
	// Location returns a human readable location of key.
	Location(key string) string
}

// NewStorage returns a Storage for dest. dest is a local directory path or an S3 URL (s3://bucket/prefix/).
func NewStorage(ctx context.Context, dest string) (Storage, error) {
	if strings.HasPrefix(dest, "s3://") {
		bucket, prefix, err := parseS3URL(dest)
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
		return NewS3Storage(newS3Client(cfg), bucket, prefix), nil
	}
	return NewLocalStorage(dest), nil
}

// newS3Client returns an S3 client. When a custom endpoint is configured
// (e.g. AWS_ENDPOINT_URL_S3 for S3 compatible storage), path-style addressing is used.
func newS3Client(cfg aws.Config) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		// optFns are applied after the base endpoint is resolved from cfg and environment variables
		if o.BaseEndpoint != nil {
			o.UsePathStyle = true
		}
	})
}

func parseS3URL(s string) (bucket, prefix string, err error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("invalid S3 URL %s: %w", s, err)
	}
	if u.Scheme != "s3" || u.Host == "" {
		return "", "", fmt.Errorf("invalid S3 URL %s: must be s3://bucket/prefix/", s)
	}
	return u.Host, strings.Trim(u.Path, "/"), nil
}

// LocalStorage stores files under a local directory.
type LocalStorage struct {
	dir string
}

func NewLocalStorage(dir string) *LocalStorage {
	return &LocalStorage{dir: dir}
}

func (s *LocalStorage) path(key string) string {
	return filepath.Join(s.dir, filepath.FromSlash(key))
}

func (s *LocalStorage) Location(key string) string {
	return s.path(key)
}

func (s *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := os.Stat(s.path(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (s *LocalStorage) Put(ctx context.Context, key string, src *os.File) error {
	dst := s.path(key)
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	// write to a temporary file and rename it to avoid leaving a partial file
	tmp, err := os.CreateTemp(dir, ".mise-mirror-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to chmod %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("failed to rename to %s: %w", dst, err)
	}
	return nil
}

// S3Client is a subset of the S3 API used by S3Storage.
type S3Client interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3Storage stores files in an S3 bucket.
type S3Storage struct {
	client S3Client
	bucket string
	prefix string
}

func NewS3Storage(client S3Client, bucket, prefix string) *S3Storage {
	return &S3Storage{client: client, bucket: bucket, prefix: strings.Trim(prefix, "/")}
}

func (s *S3Storage) objectKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return path.Join(s.prefix, key)
}

func (s *S3Storage) Location(key string) string {
	return fmt.Sprintf("s3://%s/%s", s.bucket, s.objectKey(key))
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err == nil {
		return true, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
		return false, nil
	}
	return false, fmt.Errorf("failed to head %s: %w", s.Location(key), err)
}

func (s *S3Storage) Put(ctx context.Context, key string, src *os.File) error {
	st, err := src.Stat()
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.objectKey(key)),
		Body:          src,
		ContentLength: aws.Int64(st.Size()),
		ContentType:   aws.String("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("failed to put %s: %w", s.Location(key), err)
	}
	return nil
}
