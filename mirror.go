package mirror

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
)

// Mirror downloads artifacts and stores them into Storage.
type Mirror struct {
	Storage     Storage
	HTTPClient  *http.Client
	Concurrency int
	Force       bool
	DryRun      bool
}

// ObjectKey returns the key in the mirror for rawURL. The key is "<host>/<path>",
// so that https://example.com/foo/bar.tar.gz is mirrored as example.com/foo/bar.tar.gz.
func ObjectKey(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL %s: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %s", rawURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL has no host %s", rawURL)
	}
	// path.Clean on a rooted path removes any ".." that escapes the root
	p := path.Clean("/" + u.Path)
	if p == "/" {
		return "", fmt.Errorf("URL has no path %s", rawURL)
	}
	return strings.ToLower(u.Host) + p, nil
}

// Run mirrors all artifacts. It continues on failure and returns the joined errors.
func (m *Mirror) Run(ctx context.Context, artifacts []Artifact) error {
	concurrency := max(m.Concurrency, 1)
	sem := make(chan struct{}, concurrency)
	var (
		wg                        sync.WaitGroup
		mu                        sync.Mutex
		errs                      []error
		mirrored, skipped, failed atomic.Int64
	)
	for _, a := range artifacts {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Go(func() {
			defer func() { <-sem }()
			done, err := m.mirror(ctx, a)
			switch {
			case err != nil:
				failed.Add(1)
				slog.Error("failed to mirror", "tool", a.Tool, "version", a.Version, "platform", a.Platform, "url", a.URL, "error", err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", a.URL, err))
				mu.Unlock()
			case done:
				mirrored.Add(1)
			default:
				skipped.Add(1)
			}
		})
	}
	wg.Wait()
	slog.Info("completed", "mirrored", mirrored.Load(), "skipped", skipped.Load(), "failed", failed.Load(), "dry_run", m.DryRun)
	if len(errs) > 0 {
		return fmt.Errorf("failed to mirror %d artifacts: %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// mirror mirrors an artifact. It returns true if the artifact is (or would be in dry-run) stored.
func (m *Mirror) mirror(ctx context.Context, a Artifact) (bool, error) {
	key, err := ObjectKey(a.URL)
	if err != nil {
		return false, err
	}
	loc := m.Storage.Location(key)
	log := slog.With("tool", a.Tool, "version", a.Version, "platform", a.Platform)

	if !m.Force {
		exists, err := m.Storage.Exists(ctx, key)
		if err != nil {
			return false, err
		}
		if exists {
			log.Debug("already exists, skipped", "location", loc)
			return false, nil
		}
	}
	if m.DryRun {
		log.Info("would mirror (dry-run)", "url", a.URL, "location", loc)
		return true, nil
	}

	tmp, err := os.CreateTemp("", "mise-mirror-*")
	if err != nil {
		return false, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	log.Debug("downloading", "url", a.URL)
	if err := m.download(ctx, a, tmp); err != nil {
		return false, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	if err := m.Storage.Put(ctx, key, tmp); err != nil {
		return false, err
	}
	log.Info("mirrored", "url", a.URL, "location", loc)
	return true, nil
}

func (m *Mirror) download(ctx context.Context, a Artifact, w io.Writer) error {
	h, expected, err := newChecksumHash(a.Checksum)
	if err != nil {
		slog.Warn("skip checksum verification", "url", a.URL, "error", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mise-mirror/"+Version)
	resp, err := m.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download: unexpected status %s", resp.Status)
	}

	if h != nil {
		w = io.MultiWriter(w, h)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	if h != nil {
		if got := hex.EncodeToString(h.Sum(nil)); got != expected {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", a.Checksum, got)
		}
	}
	return nil
}

// newChecksumHash returns a hash and the expected hex digest for checksum ("<algorithm>:<hex>").
// It returns a nil hash when checksum is empty.
func newChecksumHash(checksum string) (hash.Hash, string, error) {
	if checksum == "" {
		return nil, "", nil
	}
	algo, digest, ok := strings.Cut(checksum, ":")
	if !ok {
		return nil, "", fmt.Errorf("invalid checksum format %q", checksum)
	}
	digest = strings.ToLower(digest)
	switch algo {
	case "sha256":
		return sha256.New(), digest, nil
	case "sha512":
		return sha512.New(), digest, nil
	default:
		return nil, "", fmt.Errorf("unsupported checksum algorithm %q", algo)
	}
}
