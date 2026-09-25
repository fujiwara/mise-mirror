package mirror_test

import (
	"testing"

	mirror "github.com/fujiwara/mise-mirror"
)

func TestLoadLockFiles(t *testing.T) {
	artifacts, err := mirror.LoadLockFiles([]string{"testdata/mise.lock", "testdata/mise.local.lock"})
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ tool, platform, url string }
	var got []key
	for _, a := range artifacts {
		got = append(got, key{a.Tool, a.Platform, a.URL})
	}
	// jq linux-x64 appears in both files and must be deduplicated.
	// ubi:BurntSushi/ripgrep has no platforms and must be ignored.
	expected := []key{
		{"aqua:cli/cli", "linux-x64", "https://github.com/cli/cli/releases/download/v2.60.0/gh_2.60.0_linux_amd64.tar.gz"},
		{"aqua:cli/cli", "macos-arm64", "https://github.com/cli/cli/releases/download/v2.60.0/gh_2.60.0_macOS_arm64.zip"},
		{"jq", "linux-arm64", "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-arm64"},
		{"jq", "linux-x64", "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64"},
		{"jq", "macos-arm64", "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-macos-arm64"},
		{"node", "linux-x64", "https://nodejs.org/dist/v22.11.0/node-v22.11.0-linux-x64.tar.gz"},
		{"node", "macos-arm64", "https://nodejs.org/dist/v22.11.0/node-v22.11.0-darwin-arm64.tar.gz"},
	}
	if len(got) != len(expected) {
		t.Fatalf("unexpected artifacts: %v", got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("artifact[%d] = %v, want %v", i, got[i], expected[i])
		}
	}

	a := artifacts[5]
	if a.Version != "22.11.0" || a.Backend != "core:node" || a.Checksum != "sha256:4f862bab52039835efbe613b532238b6e4dde98d139a34e6923193e073438b13" {
		t.Errorf("unexpected artifact: %#v", a)
	}

	filtered := mirror.FilterPlatforms(artifacts, []string{"linux-x64"})
	if len(filtered) != 3 {
		t.Errorf("unexpected filtered artifacts: %v", filtered)
	}
	for _, a := range filtered {
		if a.Platform != "linux-x64" {
			t.Errorf("unexpected platform: %v", a)
		}
	}
}

func TestLoadLockFilesNotFound(t *testing.T) {
	if _, err := mirror.LoadLockFiles([]string{"testdata/notfound.lock"}); err == nil {
		t.Error("expected error")
	}
}
