.PHONY: test build clean

test:
	docker run --rm -v "$(CURDIR):/src" -w /src golang:1.26.7-bookworm sh -c "/usr/local/go/bin/gofmt -w cmd/cpa-provider-nexus/*.go internal/*/*.go && /usr/local/go/bin/go vet ./cmd/... ./internal/... && /usr/local/go/bin/go test ./cmd/... ./internal/..."

build:
	docker build --output type=local,dest=dist .

clean:
	rm -rf dist
