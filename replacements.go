package mirror

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// URLReplacements returns mise settings of url_replacements (in TOML) that route
// the artifact URLs to the mirror served at mirrorURL.
func URLReplacements(artifacts []Artifact, mirrorURL string) (string, error) {
	u, err := url.Parse(mirrorURL)
	if err != nil {
		return "", fmt.Errorf("invalid mirror URL %s: %w", mirrorURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid mirror URL %s: must be http(s)://host/path/", mirrorURL)
	}
	var hosts []string
	for _, a := range artifacts {
		key, err := ObjectKey(a.URL)
		if err != nil {
			continue
		}
		host, _, _ := strings.Cut(key, "/")
		hosts = append(hosts, regexp.QuoteMeta(host))
	}
	slices.Sort(hosts)
	hosts = slices.Compact(hosts)

	var b strings.Builder
	b.WriteString("[settings.url_replacements]\n")
	if len(hosts) > 0 {
		fmt.Fprintf(&b, "'regex:^https?://(%s)/' = %q\n",
			strings.Join(hosts, "|"),
			strings.TrimSuffix(mirrorURL, "/")+"/$1/",
		)
	}
	return b.String(), nil
}
