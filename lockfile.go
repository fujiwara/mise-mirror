package mirror

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Artifact is a downloadable file listed in a mise lock file.
type Artifact struct {
	Tool     string
	Version  string
	Backend  string
	Platform string
	URL      string
	Checksum string
}

type lockFile struct {
	Tools map[string][]map[string]any `toml:"tools"`
}

// ParseLockFile parses a mise lock file and returns the artifacts listed in it.
//
// Both `[tools.<name>."platforms.<platform>"]` (current format) and
// `[tools.<name>.platforms.<platform>]` (nested table) forms are supported.
func ParseLockFile(r io.Reader) ([]Artifact, error) {
	var lf lockFile
	if err := toml.NewDecoder(r).Decode(&lf); err != nil {
		return nil, err
	}
	var artifacts []Artifact
	for tool, entries := range lf.Tools {
		for _, entry := range entries {
			version, _ := entry["version"].(string)
			backend, _ := entry["backend"].(string)
			for key, value := range entry {
				var platforms map[string]any
				switch {
				case key == "platforms":
					platforms, _ = value.(map[string]any)
				case strings.HasPrefix(key, "platforms."):
					platforms = map[string]any{strings.TrimPrefix(key, "platforms."): value}
				default:
					continue
				}
				for platform, v := range platforms {
					info, ok := v.(map[string]any)
					if !ok {
						continue
					}
					url, _ := info["url"].(string)
					if url == "" {
						continue
					}
					checksum, _ := info["checksum"].(string)
					artifacts = append(artifacts, Artifact{
						Tool:     tool,
						Version:  version,
						Backend:  backend,
						Platform: platform,
						URL:      url,
						Checksum: checksum,
					})
				}
			}
		}
	}
	sortArtifacts(artifacts)
	return artifacts, nil
}

// LoadLockFiles reads mise lock files and returns the artifacts deduplicated by URL.
func LoadLockFiles(paths []string) ([]Artifact, error) {
	seen := map[string]bool{}
	var artifacts []Artifact
	for _, p := range paths {
		as, err := loadLockFile(p)
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			if seen[a.URL] {
				continue
			}
			seen[a.URL] = true
			artifacts = append(artifacts, a)
		}
	}
	sortArtifacts(artifacts)
	return artifacts, nil
}

func loadLockFile(path string) ([]Artifact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}
	defer f.Close()
	as, err := ParseLockFile(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file %s: %w", path, err)
	}
	return as, nil
}

// FilterPlatforms returns the artifacts for the given platforms. If platforms is empty, all artifacts are returned.
func FilterPlatforms(artifacts []Artifact, platforms []string) []Artifact {
	if len(platforms) == 0 {
		return artifacts
	}
	return slices.DeleteFunc(slices.Clone(artifacts), func(a Artifact) bool {
		return !slices.Contains(platforms, a.Platform)
	})
}

func sortArtifacts(artifacts []Artifact) {
	slices.SortFunc(artifacts, func(a, b Artifact) int {
		return cmp.Or(
			cmp.Compare(a.Tool, b.Tool),
			cmp.Compare(a.Version, b.Version),
			cmp.Compare(a.Platform, b.Platform),
			cmp.Compare(a.URL, b.URL),
		)
	})
}
