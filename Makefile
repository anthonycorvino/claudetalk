.PHONY: build clean server cli all

all: build

build: server cli

server:
	go build -o claudetalk-server.exe ./cmd/server

cli:
	go build -o claudetalk.exe ./cmd/claudetalk

clean:
	rm -f claudetalk-server.exe claudetalk.exe

run-server: server
	./claudetalk-server.exe --port 8080

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

run-daemon: cli
	./claudetalk.exe daemon
