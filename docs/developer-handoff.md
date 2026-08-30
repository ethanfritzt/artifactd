# Developer handoff

This note describes the implementation boundary after replacing filesystem
watching with transactional editing sessions.

## Current implementation

- `cmd/artifact` is the shell-facing CLI; `cmd/artifactd` runs the foreground
  daemon.
- The CLI talks to the daemon over a user-only Unix socket. The browser server
  is a separate localhost HTTP listener.
- Published artifacts are validated static directories. Each successful
  publish is copied into an immutable filesystem version and recorded in
  SQLite.
- `artifact create` creates a managed source directory under `sources/` and
  auto-publishes its initial scaffold when the daemon is available.
- `artifact edit begin` registers one in-memory session for an artifact.
  Agents continue to edit the source directory with normal file operations.
- `artifact edit commit` uploads the complete directory. The daemon validates
  it and publishes one version only if the session's base version is still
  current. The browser receives an SSE event and reloads the new version.

## Important invariants

- Intermediate source files are never served.
- There is at most one edit session per artifact.
- Sessions are in memory, expire after five minutes of inactivity, and are lost
  on daemon shutdown.
- A failed validation, abort, expiration, or stale-base conflict leaves the
  current published version unchanged.
- `archive`, `restore`, and daemon shutdown terminate active edit sessions as
  appropriate; restore never writes back into the agent's source directory.
- Browser routes are read-only. Registry mutations and runtime data pushes stay
  on the Unix socket.
- Publish and edit commit enforce the existing path, symlink, special-file,
  file-count, and size validation rules.

## Verification and local development

The project currently targets Linux and Go `1.25.5` or newer. Run the standard
checks before handing off a change:

```bash
make fmt
make test
make vet
make lint
make web-build
```

Build and run locally with:

```bash
make build
./bin/artifactd
```

In another terminal, use `./bin/artifact`. `make install` places both binaries
in `~/.local/bin` and installs the default Home artifact into the default data
directory. See [Configuration](configuration.md) for data, socket, URL, and
environment settings.

## Known limitations

- Editing is post-write preview, not filesystem watching or state-preserving
  HMR. A browser refresh can lose transient page state.
- A commit uploads the complete source tree and creates one immutable version;
  there is no incremental patch protocol.
- Edit sessions are not persisted and cannot resume after a daemon restart.
- The library UI does not yet expose version browsing or restore controls;
  those operations remain CLI/API functionality.
- The runtime is intentionally static and local. It does not run Node,
  package installation, arbitrary commands, or backend processes.

When changing the edit lifecycle, update `docs/protocol.md`,
`docs/agent-integration.md`, `docs/architecture.md`, and the CLI examples in
`README.md` together. Keep the protocol's stale-version and source-is-never-
served guarantees explicit.
