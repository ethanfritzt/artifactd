.PHONY: build test vet fmt lint install clean

VERSION ?= dev
LDFLAGS := -X artifactd/internal/version.Value=$(VERSION)

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/artifact ./cmd/artifact
	go build -ldflags "$(LDFLAGS)" -o bin/artifactd ./cmd/artifactd

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

lint:
	golangci-lint run ./...

install: build
	install -Dm0750 bin/artifact $(HOME)/.local/bin/artifact
	install -Dm0750 bin/artifactd $(HOME)/.local/bin/artifactd
	install -d $(HOME)/.local/share/artifactd/default
	rm -rf $(HOME)/.local/share/artifactd/default/*
	cp -a default/artifact.json default/index.html default/styles.css default/app.js $(HOME)/.local/share/artifactd/default/

clean:
	rm -rf bin coverage.out
