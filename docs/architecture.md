# Architecture

Artifactd provides a local Go CLI, daemon, and workspace-aware artifact runtime. The daemon owns the registry, immutable artifact storage, browser server, and safe host-data providers.

## Components

```text
CLI agents and humans
          │
          ├── artifact CLI ── JSON over Unix socket ──┐
          │                                            ▼
          │                                         artifactd
          │                                ┌──────────┼──────────┐
          │                                ▼          ▼          ▼
          │                            Registry   Version store  Providers
          │                            (SQLite)   (filesystem)    (system/workspace)
          │                                            │
          └── Pi/MCP/CLI data snapshots or live updates
                                                       │
                                                       ▼
                                  <id>.artifacts.localhost:7337/
                                                       ▲
                                                       │ live preview snapshots + SSE
                                                       │
                                             watched source workspace
```

There is no built-in library frontend. `artifact list` is a CLI operation, and the browser server serves published artifact files plus narrow runtime data endpoints.

## Go project structure

```text
cmd/
├── artifact/        # CLI entrypoint
└── artifactd/       # daemon entrypoint

internal/
├── cli/
├── config/
├── create/
├── daemon/
├── ipc/
├── manifest/
├── model/
├── registry/
├── providers/
│   ├── filesystem/
│   └── system/
├── live/
├── runtime/
├── storage/
└── web/
```

The project uses a modular internal structure with manual constructor wiring. No public library API or DI framework is required.

## CLI

The commands are:

```text
artifact create [artifact-id]
artifact publish <directory-or-id>
artifact list [--include-archived] [--output table|json]
artifact workspace add <id> <directory>
artifact workspace list
artifact data push <artifact-id> <source> [--file path]
artifact watch <directory>
artifact unwatch <artifact-id>
artifact live <artifact-id>
artifact versions <artifact-id>
artifact restore <artifact-id> <version>
artifact archive <artifact-id>
artifact unarchive <artifact-id>
```

`create` writes a standalone artifact scaffold into Artifactd's managed `sources/<artifact-id>/` directory by default, keeping the current repository untouched. Use `--path <directory>` for an explicit repository-local source. It supports the dependency-free `static-web` authoring stack by default and an optional React/Vite/Mantine authoring preset with opt-in features. Both target the `web-static` runtime; React projects build to static output before `publish` streams the directory to the daemon. `publish` and `watch` accept either a source directory or a managed artifact ID. `list` requests metadata from the daemon and formats it for humans or scripts; archived artifacts are omitted unless `--include-archived` is used.

The CLI uses Cobra and Viper for flags, environment variables, and optional configuration. Results go to stdout; diagnostics go to stderr. `artifact create` selects from authoring stack descriptors: the dependency-free static scaffold or the optional React/Vite/Mantine scaffold with features such as Cytoscape graph support. Stack descriptors create source projects and metadata; they do not add daemon runtime behavior.

## Daemon and IPC

`artifactd` owns all registry and storage writes. It exposes control-plane endpoints over the user-only Unix socket:

```text
GET  /v1/health
GET  /v1/artifacts[?include_archived=true]
POST /v1/artifacts/publish
GET  /v1/workspaces
POST /v1/workspaces
POST /v1/artifacts/{id}/data/{source}
POST /v1/artifacts/{id}/watch
DELETE /v1/artifacts/{id}/watch
GET /v1/artifacts/{id}/live
GET /v1/artifacts/{id}/versions
POST /v1/artifacts/{id}/restore
POST /v1/artifacts/{id}/archive
DELETE /v1/artifacts/{id}/archive
```

The browser listener exposes only artifact content, scoped runtime reads, and live-preview events. It does not expose registry mutation endpoints. For non-home HTML entrypoints, it also adds daemon-owned library navigation at response time; this platform chrome is not stored in artifact versions and is independent of the authoring stack.

These endpoints are available only over a Unix domain socket at `$XDG_RUNTIME_DIR/artifactd.sock` when that directory exists. The socket is restricted to the current user.

The browser HTTP server listens only on `127.0.0.1:7337` by default and exposes artifact content plus scoped runtime data. Control endpoints remain isolated on the Unix socket.

## Registry and storage

SQLite stores artifact metadata, archive state, current version numbers, version manifest snapshots, timestamps, content hashes, workspace definitions, and artifact-version workspace associations. Archiving changes metadata only; published files and versions remain intact. Published content is stored on the filesystem:

```text
~/.local/share/artifactd/
├── registry.db
├── staging/
├── sources/
│   └── <artifact-id>/
├── live/
│   └── <artifact-id>/snapshots/
└── artifacts/
    └── <artifact-id>/
        └── versions/
            ├── 1/
            └── 2/
```

Versions are immutable. A publish is staged, validated, moved into its final version directory atomically, and registered in an immediate SQLite write transaction. An opt-in live session separately watches an agent-owned source directory, validates atomic temporary snapshots, and serves the latest valid snapshot without changing the registry. Existing source directories and published artifacts are not moved automatically; directory-based `publish` and `watch` remain supported.

SQLite uses WAL mode, foreign keys, a busy timeout, strict tables, parameterized queries, and indexes for registry lookups.

## Routing

Artifact browser routes use an artifact-specific localhost origin:

```text
http://artifacts.localhost:7337/                 (default library artifact)
http://<id>.artifacts.localhost:7337/
http://<id>.artifacts.localhost:7337/assets/...
http://<id>.artifacts.localhost:7337/_artifactd/library
http://<id>.artifacts.localhost:7337/_artifactd/system
http://<id>.artifacts.localhost:7337/_artifactd/files
http://<id>.artifacts.localhost:7337/_artifactd/data/<source>
http://<id>.artifacts.localhost:7337/_artifactd/events
```

The legacy path form remains available for static content and redirects artifact roots to the artifact-specific origin.

## Runtime boundary

Artifacts are frontend code. Artifactd providers expose narrow, read-only data such as system metrics and workspace file metadata. The system provider reads Linux system interfaces directly and never executes shell commands. The filesystem provider is rooted at the artifact's registered workspace and rejects traversal and symlink escapes.

Pi agents can push structured JSON from CLI or MCP tools over the Unix socket. Credentials and tool execution remain inside Pi; browser code cannot invoke MCP or arbitrary commands. Optional React artifacts are built by the authoring environment into static output; artifactd never installs dependencies or executes their build commands. Runtime data and live preview snapshots are ephemeral and separate from immutable artifact versions. Live mode never serves the source directory directly; it serves a validated copied snapshot and notifies the artifact-specific browser origin through SSE.

Artifacts are treated as untrusted browser code. Artifact-specific localhost origins prevent one artifact from reading another artifact's runtime data, and filesystem paths are validated at every boundary. Writes, process control, secrets, and command execution are not ambient runtime permissions.

## Lifecycle and deployment

The daemon runs in the foreground initially:

```bash
artifactd
```

A systemd user service can supervise it later. The primary deployment is native Go binaries; Docker is not required.
