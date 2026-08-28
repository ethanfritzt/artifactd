# Development

Build both native binaries:

```bash
make build
```

Build the optional React/Mantine artifacts and their static output:

```bash
make web-build
```

Run the test suite and static checks:

```bash
make test
make vet
make lint
```

Install binaries and the built default artifact into the user data directory:

```bash
make install
```

The React authoring stack is used only while building an artifact. Artifactd serves the generated static output and never runs Node or installs packages.

The daemon runs in the foreground during development. After `make web-build`, when started from the repository root, it discovers the built default artifact under `default/dist/` automatically:

```bash
./bin/artifactd
```

Builds can embed a release version with linker flags through the `VERSION` Make variable. React templates use exact dependency versions and include a lockfile; use `npm ci` for reproducible installs in committed authoring projects.
