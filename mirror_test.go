package mirror_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mirror "github.com/fujiwara/mise-mirror"
)

func TestObjectKey(t *testing.T) {
	tests := []struct {
		url     string
		key     string
		wantErr bool
	}{
		{url: "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64", key: "github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64"},
		{url: "https://NodeJS.org/dist/v22.11.0/node.tar.gz?foo=bar", key: "nodejs.org/dist/v22.11.0/node.tar.gz"},
		{url: "http://localhost:8080/a/b", key: "localhost:8080/a/b"},
		{url: "https://example.com/../../etc/passwd", key: "example.com/etc/passwd"},
		{url: "https://example.com/", wantErr: true},
		{url: "ftp://example.com/foo", wantErr: true},
		{url: "/foo/bar", wantErr: true},
	}
	for _, tt := range tests {
		key, err := mirror.ObjectKey(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ObjectKey(%q) expected error, got %q", tt.url, key)
			}
			continue
		}
		if err != nil {
			t.Errorf("ObjectKey(%q) unexpected error: %v", tt.url, err)
			continue
		}
		if key != tt.key {
			t.Errorf("ObjectKey(%q) = %q, want %q", tt.url, key, tt.key)
		}
	}
}

func sha256sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestMirrorLocal(t *testing.T) {
	var requests atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/dl/foo.tar.gz":
			w.Write([]byte("foo"))
		case "/dl/bar.tar.gz":
			w.Write([]byte("bar"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")

	artifacts := []mirror.Artifact{
		{Tool: "foo", Platform: "linux-x64", URL: ts.URL + "/dl/foo.tar.gz", Checksum: sha256sum("foo")},
		{Tool: "bar", Platform: "linux-x64", URL: ts.URL + "/dl/bar.tar.gz"}, // no checksum
	}
	dir := t.TempDir()
	m := &mirror.Mirror{
		Storage:     mirror.NewLocalStorage(dir),
		HTTPClient:  ts.Client(),
		Concurrency: 2,
	}
	ctx := context.Background()

	// dry-run does not download
	m.DryRun = true
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("dry-run must not download: %d requests", n)
	}
	m.DryRun = false

	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"foo.tar.gz": "foo", "bar.tar.gz": "bar"} {
		b, err := os.ReadFile(filepath.Join(dir, host, "dl", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != content {
			t.Errorf("%s = %q, want %q", name, b, content)
		}
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("unexpected requests: %d", n)
	}

	// already mirrored files are skipped
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("existing files must be skipped: %d requests", n)
	}

	// --force downloads again
	m.Force = true
	if err := m.Run(ctx, artifacts); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 4 {
		t.Errorf("--force must download again: %d requests", n)
	}
}

func TestMirrorErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("ok"))
		case "/bad-checksum":
			w.Write([]byte("tampered"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")

	artifacts := []mirror.Artifact{
		{Tool: "bad", URL: ts.URL + "/bad-checksum", Checksum: sha256sum("original")},
		{Tool: "notfound", URL: ts.URL + "/notfound"},
		{Tool: "ok", URL: ts.URL + "/ok", Checksum: sha256sum("ok")},
	}
	dir := t.TempDir()
	m := &mirror.Mirror{
		Storage:    mirror.NewLocalStorage(dir),
		HTTPClient: ts.Client(),
	}
	err := m.Run(context.Background(), artifacts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") || !strings.Contains(err.Error(), "404") {
		t.Errorf("unexpected error: %v", err)
	}
	// failed downloads must not be stored, and the others must be mirrored
	if _, err := os.Stat(filepath.Join(dir, host, "bad-checksum")); !os.IsNotExist(err) {
		t.Errorf("file with bad checksum must not be stored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, host, "ok")); err != nil {
		t.Errorf("ok must be mirrored: %v", err)
	}
}

type recordTransport struct {
	mu      sync.Mutex
	headers map[string]string // URL -> Authorization header
}

func (t *recordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.headers[req.URL.String()] = req.Header.Get("Authorization")
	t.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

func TestMirrorGitHubToken(t *testing.T) {
	urls := []string{
		"https://github.com/owner/repo/releases/download/v1.0.0/foo.tar.gz",
		"https://api.github.com/repos/owner/repo/releases/assets/1",
		"https://nodejs.org/dist/v22.11.0/node.tar.gz",
		"https://github.com.example.com/foo",
		"https://objects.githubusercontent.com/foo",
	}
	var artifacts []mirror.Artifact
	for _, u := range urls {
		artifacts = append(artifacts, mirror.Artifact{URL: u})
	}
	for _, token := range []string{"", "secret"} {
		tr := &recordTransport{headers: map[string]string{}}
		m := &mirror.Mirror{
			Storage:     mirror.NewLocalStorage(t.TempDir()),
			HTTPClient:  &http.Client{Transport: tr},
			GitHubToken: token,
		}
		if err := m.Run(context.Background(), artifacts); err != nil {
			t.Fatal(err)
		}
		for i, u := range urls {
			want := ""
			if token != "" && i < 2 {
				want = "Bearer " + token
			}
			if got := tr.headers[u]; got != want {
				t.Errorf("token=%q: Authorization for %s = %q, want %q", token, u, got, want)
			}
		}
	}
}

func TestGitHubTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if got := mirror.GitHubTokenFromEnv(); got != "" {
		t.Errorf("got %q", got)
	}
	t.Setenv("GH_TOKEN", "gh")
	if got := mirror.GitHubTokenFromEnv(); got != "gh" {
		t.Errorf("got %q, want gh", got)
	}
	t.Setenv("GITHUB_TOKEN", "github")
	if got := mirror.GitHubTokenFromEnv(); got != "github" {
		t.Errorf("got %q, want github", got)
	}
}
