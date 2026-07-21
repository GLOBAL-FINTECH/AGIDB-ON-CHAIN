.PHONY: build test windows linux
build:
	go build -trimpath -ldflags="-s -w" -o dist/agidb ./cmd/agidb

test:
	go test ./...

linux:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/agidb-linux-amd64 ./cmd/agidb

windows:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/agidb-windows-amd64.exe ./cmd/agidb
