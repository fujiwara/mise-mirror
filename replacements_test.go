package mirror_test

import (
	"testing"

	mirror "github.com/fujiwara/mise-mirror"
)

func TestURLReplacements(t *testing.T) {
	artifacts, err := mirror.LoadLockFiles([]string{"testdata/mise.lock", "testdata/mise.local.lock"})
	if err != nil {
		t.Fatal(err)
	}
	artifacts = append(artifacts, mirror.Artifact{URL: "http://localhost:8080/foo/bar"})

	expected := `[settings.url_replacements]
'regex:^https?://(github\.com|localhost:8080|nodejs\.org)/' = "https://mirror.example.com/prefix/$1/"
`
	for _, mirrorURL := range []string{"https://mirror.example.com/prefix/", "https://mirror.example.com/prefix"} {
		got, err := mirror.URLReplacements(artifacts, mirrorURL)
		if err != nil {
			t.Fatal(err)
		}
		if got != expected {
			t.Errorf("URLReplacements(%q) =\n%s\nwant\n%s", mirrorURL, got, expected)
		}
	}

	for _, mirrorURL := range []string{"s3://bucket/prefix/", "mirror.example.com", "://"} {
		if _, err := mirror.URLReplacements(artifacts, mirrorURL); err == nil {
			t.Errorf("URLReplacements(%q) expected error", mirrorURL)
		}
	}
}
