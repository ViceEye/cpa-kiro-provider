FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN /usr/local/go/bin/go mod download

COPY . .
RUN /usr/local/go/bin/gofmt -w cmd/kiro-provider/*.go internal/*/*.go \
    && /usr/local/go/bin/go vet ./... \
    && /usr/local/go/bin/go test ./... \
    && mkdir -p /out/linux/amd64 \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
       -buildvcs=false -trimpath -buildmode=c-shared \
       -ldflags="-s -w" \
       -o /out/linux/amd64/kiro-provider-v0.7.8.so ./cmd/kiro-provider \
    && rm -f /out/linux/amd64/*.h \
    && cd /out/linux/amd64 \
    && sha256sum kiro-provider-v0.7.8.so > kiro-provider-v0.7.8.so.sha256

FROM scratch
COPY --from=builder /out/ /
