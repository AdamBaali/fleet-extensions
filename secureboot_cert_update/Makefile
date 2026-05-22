SRC=main.go

all: windows

windows: secureboot_cert_update-amd64.exe secureboot_cert_update-arm64.exe

secureboot_cert_update-amd64.exe: $(SRC)
	GOARCH=amd64 GOOS=windows go build -o secureboot_cert_update-amd64.exe $(SRC)

secureboot_cert_update-arm64.exe: $(SRC)
	GOARCH=arm64 GOOS=windows go build -o secureboot_cert_update-arm64.exe $(SRC)

build: all

clean:
	rm -f secureboot_cert_update-amd64.exe secureboot_cert_update-arm64.exe

deps:
	go mod tidy

tidy:
	go mod tidy
