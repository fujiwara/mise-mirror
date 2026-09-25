package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/alecthomas/kong"
	"github.com/fujiwara/sloghandler"
)

func Run(ctx context.Context) error {
	var c CLI
	k, err := kong.New(&c,
		kong.Name("mise-mirror"),
		kong.Description("Mirror tool binaries listed in mise lock files to a local directory or S3."),
		kong.Vars{"version": fmt.Sprintf("mise-mirror %s", Version)},
	)
	if err != nil {
		return fmt.Errorf("failed to create parser: %w", err)
	}
	if _, err := k.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}
	c.w = os.Stdout

	level := slog.LevelInfo
	if c.Debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(sloghandler.NewLogHandler(os.Stderr, &sloghandler.HandlerOptions{
		HandlerOptions: slog.HandlerOptions{Level: level},
		Color:          true,
	})))

	return run(ctx, &c)
}

func run(ctx context.Context, c *CLI) error {
	if c.MirrorURL != "" {
		// validate before mirroring
		if _, err := URLReplacements(nil, c.MirrorURL); err != nil {
			return err
		}
	}
	artifacts, err := LoadLockFiles(c.LockFiles)
	if err != nil {
		return err
	}
	artifacts = FilterPlatforms(artifacts, c.Platforms)

	storage, err := NewStorage(ctx, c.Destination)
	if err != nil {
		return err
	}
	m := &Mirror{
		Storage:     storage,
		HTTPClient:  http.DefaultClient,
		Concurrency: c.Concurrency,
		Force:       c.Force,
		DryRun:      c.DryRun,
	}
	if err := m.Run(ctx, artifacts); err != nil {
		return err
	}

	if c.MirrorURL != "" {
		s, err := URLReplacements(artifacts, c.MirrorURL)
		if err != nil {
			return err
		}
		fmt.Fprint(c.w, s)
	}
	return nil
}
