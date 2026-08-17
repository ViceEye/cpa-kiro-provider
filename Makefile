.PHONY: test build clean

test:
	docker run --rm -v "$(CURDIR):/src" -w /src golang:1.26-bookworm sh -c "/usr/local/go/bin/gofmt -w . && /usr/local/go/bin/go test ./..."

build:
	docker build --output type=local,dest=dist .

clean:
	rm -rf dist
