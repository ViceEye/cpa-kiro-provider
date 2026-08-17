FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN /usr/local/go/bin/go mod download

COPY . .
RUN /usr/local/go/bin/gofmt -w . \
    && /usr/local/go/bin/go test ./... \
    && mkdir -p /out/linux/amd64 \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
       -buildvcs=false -trimpath -buildmode=c-shared \
       -ldflags="-s -w" \
	       -o /out/linux/amd64/kiro-provider-v0.5.6.so . \
	    && rm -f /out/linux/amd64/kiro-provider-v0.5.6.h

FROM scratch
COPY --from=builder /out/ /
