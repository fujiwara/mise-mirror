package mirror_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	mirror "github.com/fujiwara/mise-mirror"
)

type fakeS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (c *fakeS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.objects[aws.ToString(params.Bucket)+"/"+aws.ToString(params.Key)]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func (c *fakeS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[aws.ToString(params.Bucket)+"/"+aws.ToString(params.Key)] = b
	return &s3.PutObjectOutput{}, nil
}

func writeTempFile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "src")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return f
}

func testStorage(t *testing.T, s mirror.Storage) {
	t.Helper()
	ctx := context.Background()
	key := "example.com/foo/bar.tar.gz"
	if ok, err := s.Exists(ctx, key); err != nil || ok {
		t.Fatalf("Exists before Put = %v, %v", ok, err)
	}
	if err := s.Put(ctx, key, writeTempFile(t, "hello")); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Exists(ctx, key); err != nil || !ok {
		t.Fatalf("Exists after Put = %v, %v", ok, err)
	}
}

func TestLocalStorage(t *testing.T) {
	dir := t.TempDir()
	s := mirror.NewLocalStorage(dir)
	testStorage(t, s)
	b, err := os.ReadFile(filepath.Join(dir, "example.com", "foo", "bar.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Errorf("unexpected content %q", b)
	}
}

func TestS3Storage(t *testing.T) {
	for _, prefix := range []string{"", "mirror", "/mirror/", "a/b/"} {
		t.Run(prefix, func(t *testing.T) {
			client := &fakeS3Client{objects: map[string][]byte{}}
			s := mirror.NewS3Storage(client, "bucket", prefix)
			testStorage(t, s)
			var want string
			switch prefix {
			case "":
				want = "bucket/example.com/foo/bar.tar.gz"
			case "a/b/":
				want = "bucket/a/b/example.com/foo/bar.tar.gz"
			default:
				want = "bucket/mirror/example.com/foo/bar.tar.gz"
			}
			if b, ok := client.objects[want]; !ok || string(b) != "hello" {
				t.Errorf("object %s not found: %v", want, client.objects)
			}
		})
	}
}

func TestParseS3URL(t *testing.T) {
	tests := []struct {
		url, bucket, prefix string
		wantErr             bool
	}{
		{url: "s3://bucket", bucket: "bucket"},
		{url: "s3://bucket/", bucket: "bucket"},
		{url: "s3://bucket/prefix/", bucket: "bucket", prefix: "prefix"},
		{url: "s3://bucket/a/b", bucket: "bucket", prefix: "a/b"},
		{url: "s3:///prefix", wantErr: true},
	}
	for _, tt := range tests {
		bucket, prefix, err := mirror.ParseS3URL(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseS3URL(%q) expected error", tt.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseS3URL(%q) unexpected error: %v", tt.url, err)
			continue
		}
		if bucket != tt.bucket || prefix != tt.prefix {
			t.Errorf("ParseS3URL(%q) = %q, %q, want %q, %q", tt.url, bucket, prefix, tt.bucket, tt.prefix)
		}
	}
}

func TestNewS3ClientPathStyle(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		pathStyle bool
	}{
		{name: "default", pathStyle: false},
		{name: "AWS_ENDPOINT_URL_S3", env: map[string]string{"AWS_ENDPOINT_URL_S3": "http://localhost:7070"}, pathStyle: true},
		{name: "AWS_ENDPOINT_URL", env: map[string]string{"AWS_ENDPOINT_URL": "http://localhost:7070"}, pathStyle: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_ENDPOINT_URL", "")
			os.Unsetenv("AWS_ENDPOINT_URL")
			t.Setenv("AWS_ENDPOINT_URL_S3", "")
			os.Unsetenv("AWS_ENDPOINT_URL_S3")
			t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
			t.Setenv("AWS_REGION", "us-east-1")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := config.LoadDefaultConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			client := mirror.NewS3Client(cfg)
			if got := client.Options().UsePathStyle; got != tt.pathStyle {
				t.Errorf("UsePathStyle = %v, want %v", got, tt.pathStyle)
			}
		})
	}
}
