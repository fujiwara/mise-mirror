.PHONY: clean test

mise-mirror: go.* *.go
	go build -o $@ ./cmd/mise-mirror

clean:
	rm -rf mise-mirror dist/

test:
	go test -v ./...

install:
	go install github.com/fujiwara/mise-mirror/cmd/mise-mirror

dist:
	goreleaser build --snapshot --clean
