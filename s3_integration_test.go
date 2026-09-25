package mirror_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	mirror "github.com/fujiwara/mise-mirror"
)

// TestS3Integration mirrors files to a real S3 compatible server.
// It runs only when MISE_MIRROR_TEST_S3_BUCKET is set. The endpoint and credentials
// are configured by the standard AWS environment variables (e.g. AWS_ENDPOINT_URL_S3).
func TestS3Integration(t *testing.T) {
	bucket := os.Getenv("MISE_MIRROR_TEST_S3_BUCKET")
	if bucket == "" {
		t.Skip("MISE_MIRROR_TEST_S3_BUCKET is not set")
	}
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := mirror.NewS3Client(cfg)
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if !errors.As(err, &owned) && !errors.As(err, &exists) {
			t.Fatal(err)
		}
	}

	var requests atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte("content of " + r.URL.Path))
	}))
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")

	artifacts := []mirror.Artifact{
		{Tool: "foo", URL: ts.URL + "/dl/foo.tar.gz", Checksum: sha256sum("content of /dl/foo.tar.gz")},
		{Tool: "bar", URL: ts.URL + "/dl/v1.0.0/bar"},
	}
	prefix := fmt.Sprintf("test/%d", time.Now().UnixNano())
	storage, err := mirror.NewStorage(ctx, fmt.Sprintf("s3://%s/%s/", bucket, prefix))
	if err != nil {
		t.Fatal(err)
	}
	m := &mirror.Mirror{
		Storage:     storage,
		HTTPClient:  ts.Client(),
		Concurrency: 2,
	}
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/dl/foo.tar.gz", "/dl/v1.0.0/bar"} {
		key := prefix + "/" + host + p
		out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err != nil {
			t.Fatalf("failed to get %s: %v", key, err)
		}
		b, err := io.ReadAll(out.Body)
		out.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "content of "+p {
			t.Errorf("unexpected content of %s: %q", key, b)
		}
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("unexpected requests: %d", n)
	}

	// already mirrored objects are skipped
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("existing objects must be skipped: %d requests", n)
	}

	// --force uploads again
	m.Force = true
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 4 {
		t.Errorf("--force must download again: %d requests", n)
	}
}
