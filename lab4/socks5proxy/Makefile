.PHONY: build test run vet

build:
	go build -o bin/socks5proxy ./cmd/socks5proxy

test:
	go test ./...

vet:
	go vet ./...

run: build
	./bin/socks5proxy 1080
