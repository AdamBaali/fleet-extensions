.PHONY: build clean test deps all vet fmt

# Non-test Go sources; binaries are rebuilt when these change.
GO_SOURCES := main.go

all: rpm_ostree-amd64.ext rpm_ostree-arm64.ext

rpm_ostree-amd64.ext: $(GO_SOURCES)
	GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o rpm_ostree-amd64.ext .

rpm_ostree-arm64.ext: $(GO_SOURCES)
	GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o rpm_ostree-arm64.ext .

build: all

clean:
	rm -f rpm_ostree-amd64.ext rpm_ostree-arm64.ext

deps:
	go mod tidy

test:
	go test -v ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
