.PHONY: build web-build test vet fmt lint install clean

VERSION ?= dev
LDFLAGS := -X artifactd/internal/version.Value=$(VERSION)

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/artifact ./cmd/artifact
	go build -ldflags "$(LDFLAGS)" -o bin/artifactd ./cmd/artifactd

web-build:
	npm --prefix default ci
	npm --prefix default run build
	npm --prefix examples/top-lite ci
	npm --prefix examples/top-lite run build

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

lint:
	golangci-lint run ./...

install: build web-build
	install -Dm0750 bin/artifact $(HOME)/.local/bin/artifact
	install -Dm0750 bin/artifactd $(HOME)/.local/bin/artifactd
	install -d $(HOME)/.local/share/artifactd/default
	cp -a default/dist/. $(HOME)/.local/share/artifactd/default/

clean:
	rm -rf bin coverage.out
