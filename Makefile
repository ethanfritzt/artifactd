.PHONY: build web-build test vet fmt lint install clean

VERSION ?= dev
LDFLAGS := -X artifactd/internal/version.Value=$(VERSION)

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/artifact ./cmd/artifact
	go build -ldflags "$(LDFLAGS)" -o bin/artifactd ./cmd/artifactd

# Static artifacts do not require a Node build. Keep this target as the
# repository/CI validation hook for the files that are served directly.
web-build:
	@set -eu; for directory in default examples/top-lite; do \
		test -s "$$directory/artifact.json"; \
		test -s "$$directory/index.html"; \
	done

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
	$(HOME)/.local/bin/artifact integration install

clean:
	rm -rf bin coverage.out
