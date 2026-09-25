package mirror

import (
	"io"

	"github.com/alecthomas/kong"
)

type CLI struct {
	Destination string   `arg:"" help:"Destination directory or S3 URL (e.g. /path/to/dir, s3://bucket/prefix/)"`
	LockFiles   []string `name:"lock-file" short:"l" help:"Path to mise lock file. Can be specified multiple times." default:"mise.lock" env:"MISE_MIRROR_LOCK_FILE"`
	Platforms   []string `name:"platform" short:"p" help:"Mirror only the specified platforms (e.g. linux-x64). Can be specified multiple times. Default: all platforms." env:"MISE_MIRROR_PLATFORM"`
	Concurrency int      `help:"Number of concurrent downloads." default:"4" env:"MISE_MIRROR_CONCURRENCY"`
	Force       bool     `help:"Overwrite files that already exist in the destination." env:"MISE_MIRROR_FORCE"`
	DryRun      bool     `help:"Show what would be mirrored without downloading." env:"MISE_MIRROR_DRY_RUN"`
	Debug       bool     `help:"Enable debug logging." env:"MISE_MIRROR_DEBUG"`
	MirrorURL   string   `name:"mirror-url" help:"Base URL where the destination is served (e.g. https://mirror.example.com/prefix/). If specified, print url_replacements settings for mise after mirroring." env:"MISE_MIRROR_URL"`

	Version kong.VersionFlag `short:"v" help:"Show version and exit."`

	w io.Writer `kong:"-"`
}
