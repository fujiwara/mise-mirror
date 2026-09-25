# mise-mirror

mise-mirror mirrors tool binaries listed in [mise](https://mise.en.dev/) lock files (`mise.lock`) to a local directory or an Amazon S3 bucket.

Combined with mise's [`url_replacements`](https://mise.en.dev/url-replacements.html) setting, you can install tools from your own mirror instead of upstream servers (GitHub Releases, nodejs.org, etc.).

## Install

```console
$ go install github.com/fujiwara/mise-mirror/cmd/mise-mirror@latest
```

Or download a binary from [Releases](https://github.com/fujiwara/mise-mirror/releases).

## Usage

```
Usage: mise-mirror <destination> [flags]

Mirror tool binaries listed in mise lock files to a local directory or S3.

Arguments:
  <destination>    Destination directory or S3 URL (e.g. /path/to/dir,
                   s3://bucket/prefix/)

Flags:
  -h, --help                       Show context-sensitive help.
  -l, --lock-file=mise.lock,...    Path to mise lock file. Can be specified
                                   multiple times ($MISE_MIRROR_LOCK_FILE).
  -p, --platform=PLATFORM,...      Mirror only the specified platforms
                                   (e.g. linux-x64). Can be specified
                                   multiple times. Default: all platforms
                                   ($MISE_MIRROR_PLATFORM).
      --concurrency=4              Number of concurrent downloads
                                   ($MISE_MIRROR_CONCURRENCY).
      --force                      Overwrite files that already exist in the
                                   destination ($MISE_MIRROR_FORCE).
      --dry-run                    Show what would be mirrored without
                                   downloading ($MISE_MIRROR_DRY_RUN).
      --debug                      Enable debug logging ($MISE_MIRROR_DEBUG).
      --mirror-url=STRING          Base URL where the destination is served
                                   (e.g. https://mirror.example.com/prefix/).
                                   If specified, print url_replacements settings
                                   for mise after mirroring ($MISE_MIRROR_URL).
  -v, --version                    Show version and exit.
```

### Examples

Mirror to a local directory:

```console
$ mise-mirror --lock-file mise.lock --lock-file mise.local.lock /path/to/dir
```

Mirror to S3 (credentials are loaded by the AWS SDK default chain):

```console
$ mise-mirror s3://my-bucket/mise-mirror/
```

S3 compatible storage can be used by setting the endpoint with the AWS SDK environment variables (`AWS_ENDPOINT_URL_S3` or `AWS_ENDPOINT_URL`) or `endpoint_url` in the shared config file. When a custom endpoint is configured, path-style addressing (`http://endpoint/bucket/key`) is used. Otherwise (Amazon S3), the SDK default addressing is used.

```console
$ AWS_ENDPOINT_URL_S3=http://localhost:7070 mise-mirror s3://my-bucket/mise-mirror/
```

Mirror only specific platforms:

```console
$ mise-mirror --platform linux-x64 --platform linux-arm64 s3://my-bucket/mise-mirror/
```

### GitHub token

If `GITHUB_TOKEN` (or `GH_TOKEN`) is set, it is sent as the `Authorization` header to `github.com` and `api.github.com` to avoid rate limits for unauthenticated requests. The token is not sent to other hosts, including redirect destinations of GitHub release downloads.

```console
$ GITHUB_TOKEN=$(gh auth token) mise-mirror /path/to/dir
```

### Layout of the mirror

Each file is stored as `<destination>/<host>/<path>` of the `url` in the lock file.

```toml
[tools.jq."platforms.linux-x64"]
checksum = "sha256:5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5"
url = "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64"
```

is mirrored to

```
<destination>/github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64
```

- Downloaded files are verified by `checksum` in the lock file (`sha256` and `sha512` are supported). Files with mismatched checksums are not stored.
- Files that already exist in the destination are skipped. Use `--force` to download them again.
- The same URL in multiple lock files is mirrored only once.
- Query strings in URLs are ignored.

## Using the mirror from mise

Serve the destination over HTTP(S) (e.g. S3 + CloudFront, or any static web server), then configure `url_replacements` to route download URLs to the mirror.

```toml
[settings.url_replacements]
'regex:^https?://(github\.com|nodejs\.org)/' = "https://mise-mirror.example.com/$1/"
```

With `--mirror-url`, mise-mirror prints these settings to stdout after mirroring, covering all hosts that appear in the lock files.

```console
$ mise-mirror --mirror-url https://mise-mirror.example.com/ s3://my-bucket/
[settings.url_replacements]
'regex:^https?://(github\.com|nodejs\.org)/' = "https://mise-mirror.example.com/$1/"
```

If you write the settings by hand, list the hosts that appear in your lock files. Do not route every `https://` URL to the mirror, because mise also sends API requests (e.g. `api.github.com`) through `url_replacements`, and those are not mirrored.

To make sure that tools are installed from the lock file URLs, use `mise install --locked` (or `MISE_LOCKED=1`).

## Limitations

- Only `url` in lock files is mirrored. `url_api` (GitHub API asset URLs) is not mirrored.
- Tools without platform URLs in the lock file (e.g. installed by plugins or package managers) are not mirrored.

## Development

```console
$ make test
```

The S3 integration test runs against an S3 compatible server only when `MISE_MIRROR_TEST_S3_BUCKET` is set. For example, with [versitygw](https://github.com/versity/versitygw) (posix backend):

```console
$ mkdir -p /tmp/versitygw-data
$ ROOT_ACCESS_KEY=testkey ROOT_SECRET_KEY=testsecret versitygw --port 127.0.0.1:7070 posix /tmp/versitygw-data &
$ AWS_ACCESS_KEY_ID=testkey AWS_SECRET_ACCESS_KEY=testsecret AWS_REGION=us-east-1 \
  AWS_ENDPOINT_URL_S3=http://localhost:7070 MISE_MIRROR_TEST_S3_BUCKET=mise-mirror-test \
  go test -run TestS3Integration -v ./...
```

The bucket is created by the test if it does not exist.

## LICENSE

MIT

## Author

FUJIWARA Shunichiro
