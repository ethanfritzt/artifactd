# Development

Build both native binaries:

```bash
make build
```

Run the test suite and static checks:

```bash
make test
make vet
make lint
```

Install binaries and the raw default artifact into the user data directory:

```bash
make install
```

Artifacts in this repository are standalone HTML, CSS, and JavaScript files. The daemon serves them directly and never runs Node, installs packages, or executes application builds.

The daemon runs in the foreground during development. When started from the repository root, it discovers the raw default artifact under `default/` automatically:

```bash
./bin/artifactd
```

Builds can embed a release version with linker flags through the `VERSION` Make variable.
