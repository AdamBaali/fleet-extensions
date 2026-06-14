.PHONY: build clean test deps all

# Static, CGO-free builds so the .ext runs on any glibc/musl atomic host.
GO_BUILD = CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w"

all: rpm_ostree-amd64.ext rpm_ostree-arm64.ext

rpm_ostree-amd64.ext:
	GOARCH=amd64 $(GO_BUILD) -o rpm_ostree-amd64.ext .

rpm_ostree-arm64.ext:
	GOARCH=arm64 $(GO_BUILD) -o rpm_ostree-arm64.ext .

build: all

clean:
	rm -f rpm_ostree-amd64.ext rpm_ostree-arm64.ext

deps:
	go mod tidy

test:
	go test ./...
