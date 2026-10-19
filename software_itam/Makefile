EXTENSION := software_itam
PKG       := .
# CGO is off so every target cross-compiles from one runner.
GOBUILD   := CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"

BINARIES := $(EXTENSION)-x86_64.ext $(EXTENSION)-arm64.ext \
	$(EXTENSION)-amd64.ext $(EXTENSION)-linux-arm64.ext \
	$(EXTENSION)-amd64.exe $(EXTENSION)-arm64.exe

.PHONY: all build darwin linux windows deps tidy test vet fmt checksums clean

all: build

build: darwin linux windows

darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(EXTENSION)-x86_64.ext $(PKG)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(EXTENSION)-arm64.ext $(PKG)

linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(EXTENSION)-amd64.ext $(PKG)
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(EXTENSION)-linux-arm64.ext $(PKG)

windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(EXTENSION)-amd64.exe $(PKG)
	GOOS=windows GOARCH=arm64 $(GOBUILD) -o $(EXTENSION)-arm64.exe $(PKG)

# The GitOps installers pin these hashes, so print them after every build.
checksums:
	sha256sum $(BINARIES)

deps tidy:
	go mod tidy

test:
	go test -v ./...

vet:
	for os in darwin linux windows; do GOOS=$$os go vet ./... || exit 1; done

fmt:
	gofmt -w .

clean:
	rm -f $(BINARIES)
