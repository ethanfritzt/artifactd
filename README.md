# artifactd

> A local, agent-agnostic runtime for persistent interactive artifacts on Linux.

![Artifactd Home library](docs/images/artifactd-home.png)

**Status: MVP implemented; pre-alpha.** The repository contains the Go CLI, daemon, SQLite registry, immutable filesystem version store, workspace providers, artifact runtime data APIs, transactional editing, and dependency-free static artifact authoring.

## Why artifactd?

CLI agents can create useful software quickly, but the result usually ends up as a file that users must find, host, and remember how to reopen. `artifactd` gives those generated tools a durable local home.

An agent—or a human—can publish a standalone interactive artifact and get a stable local URL:

```text
artifact publish house-budget

✓ Published House Budget
  http://house-budget.artifacts.localhost:7337/
```

The artifact remains available in a local library until it is archived. Archiving hides it from the active library without deleting its stable URL or version history.

## Product boundaries

`artifactd` is **not** an AI agent, IDE, deployment platform, or hosted application service. Agents such as Pi, Claude Code, Codex, and OpenCode create artifacts; `artifactd` stores, versions, serves, and organizes them.

The current runtime is deliberately local-first:

- zero account and zero cloud dependency
- standalone HTML, CSS, JavaScript, and SVG artifacts
- no Node server, package installation, Docker, or backend required for the MVP
- stable local URLs and a CLI-managed artifact registry
- workspace-scoped filesystem data and read-only system metrics

## Workflow

```text
CLI agent or human
        │
        ▼
artifact create --id artifact-id
        │
        ├── editable source under Artifactd's managed data directory
        └── initial scaffold published automatically
        │
        ▼
artifact edit begin → ordinary source edits → artifact edit commit
        │
        └── complete source is validated and published as one new version
        │
        ▼
http://artifact-id.artifacts.localhost:7337/
```

Publishing the same artifact again updates its existing entry and creates a new
immutable version rather than overwriting history. Archived artifacts remain
archived until explicitly unarchived.

## Current MVP

The current MVP answers one question: **does it feel valuable when an agent-generated tool permanently exists in a local library?**

Current capabilities:

- a small `artifact.json` manifest
- standalone static artifacts, including rendered Markdown entrypoints
- dependency-free HTML, CSS, and JavaScript scaffolding via `artifact create --id <id>`
- temporary Markdown previews with source editing, safe save, PDF print, and DOCX export via `artifact preview <file> --open`
- `artifact create`, `publish`, `list`, `archive`, `unarchive`, `edit`, `versions`, and `restore` (`create` auto-publishes; `--open` launches the result)
- Go `artifactd` daemon with JSON-over-Unix-socket IPC
- SQLite-backed metadata registry
- filesystem-backed immutable versions
- artifact-specific localhost origins
- scoped workspace filesystem metadata
- live system metrics, agent-pushed JSON data, and transactional agent editing with automatic browser refresh
- a documented shell-first integration for agents

The MVP intentionally excludes AI chat, an IDE, arbitrary shell access, Node runtimes, cloud hosting, authentication, collaboration, plugin marketplaces, permanent artifact purge, and state-preserving HMR.

## Documentation

- [Architecture](docs/architecture.md) — implemented runtime boundaries and data flow
- [Protocol](docs/protocol.md) — CLI-to-daemon API
- [Artifact specification](docs/artifact-spec.md) — versioned artifact metadata and packaging conventions
- [Security model](docs/security.md) — threat model and constraints for generated content
- [Agent integration](docs/agent-integration.md) — shell-first integration and future directions
- [Default artifact](default/) — the built-in library starter/template artifact
- [Roadmap](docs/roadmap.md) — staged evolution beyond the MVP
- [Development](docs/development.md) — build, install, and test commands
- [Configuration](docs/configuration.md) — flags, environment variables, paths, and defaults
- [Artifact authoring](docs/authoring.md) — HTML, CSS, JavaScript, and Markdown workflows
- [Contributing](CONTRIBUTING.md) — how to contribute while the project is still being shaped
- [Developer handoff](docs/developer-handoff.md) — implementation invariants, verification, and known limitations

These documents describe the current pre-alpha API and implementation boundaries. Runtime providers are intentionally narrow, while publishing and transactional editing are implemented.

## Quick start

Build the two native Linux binaries:

```bash
make build
```

Start the daemon, create an artifact, open its initial preview, and list it:

```bash
./bin/artifactd &
./bin/artifact create --id demo --open
./bin/artifact preview ./README.md --open
./bin/artifact list
```

The defaults use `$XDG_DATA_HOME/artifactd` (or
`~/.local/share/artifactd`) for data, a user-local Unix socket, and
`127.0.0.1:7337` for browser traffic. See
[Configuration](docs/configuration.md) to change these locations or use
`ARTIFACTD_*` environment variables.

`artifact create` stores editable source outside the current repository, under
Artifactd's managed data directory. Use `--path ./demo --id demo` when a
repository-local source directory is intentional. Existing directory paths
remain supported by `publish`.

Create publishes the generated static files immediately. For an edit, begin a transaction before changing files and commit it when the complete source is ready:

```bash
source_dir="$(./bin/artifact create --id my-tool)"
./bin/artifact edit begin my-tool --message "Building the tool" --output json
# record the returned session_id, then edit files in "$source_dir"
./bin/artifact edit commit my-tool --session <session-id>
```

The browser shows a loading overlay during the transaction and refreshes after the validated immutable version is published.

Published artifacts are served at `http://<id>.artifacts.localhost:7337/`. The default Artifactd Home library is available at `http://artifacts.localhost:7337/`. Legacy path URLs remain available and redirect at the artifact root.

On Linux, add Artifactd to GNOME Files and other MIME-aware file managers without changing the default Markdown application:

```bash
./bin/artifact integration install
```

Markdown files then offer **Artifactd Markdown Preview** under **Open With**. Remove the entry with `artifact integration uninstall`.

## Design principles

1. **Agent agnostic** — agents, scripts, and humans are all clients.
2. **Local first** — the default experience needs no account, cloud, or deployment.
3. **Instant** — opening an artifact should feel like opening a document, not starting a development server.
4. **Safe by default** — generated content must not receive unrestricted host access.
5. **Disposable but durable** — creation and deletion should be easy, while useful artifacts can live for years.

## Current repository shape

```text
.
├── cmd/
│   ├── artifact/
│   └── artifactd/
├── internal/
├── docs/
├── default/              # built-in Artifactd Home artifact
├── examples/
│   └── top-lite/
├── Makefile
├── go.mod
└── go.sum
```

The runtime implementation is intentionally small. Implementation choices should continue to follow product validation rather than speculative platform features.
