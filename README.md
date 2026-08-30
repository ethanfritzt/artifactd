# artifactd

> A local, agent-agnostic runtime for persistent interactive artifacts on Linux.

![Artifactd Home library](docs/images/artifactd-home.png)

**Status: pre-alpha / workspace runtime implementation.** The repository contains the Go CLI, daemon, SQLite registry, immutable filesystem version store, workspace providers, artifact runtime data APIs, and dependency-free static artifact authoring.

## Why artifactd?

CLI agents can create useful software quickly, but the result usually ends up as a file that users must find, host, and remember how to reopen. `artifactd` is intended to give those generated tools a durable local home.

An agent—or a human—should be able to publish a standalone interactive artifact and get a stable local URL:

```text
artifact publish house-budget

✓ Published House Budget
  http://house-budget.artifacts.localhost:7337/
```

The artifact remains available in a local library until it is archived. Archiving hides it from the active library without deleting its stable URL or version history.

## Product boundaries

`artifactd` is **not** an AI agent, IDE, deployment platform, or hosted application service. Agents such as Pi, Claude Code, Codex, and OpenCode create artifacts; `artifactd` stores, versions, serves, and organizes them.

The initial direction is deliberately local-first:

- zero account and zero cloud dependency
- standalone HTML, CSS, JavaScript, and SVG artifacts
- no Node server, package installation, Docker, or backend required for the MVP
- stable local URLs and a CLI-managed artifact registry
- workspace-scoped filesystem data and read-only system metrics

## Proposed workflow

```text
CLI agent or human
        │
        ▼
artifact create --id artifact-id
        │
        ├── editable source under ~/.local/share/artifactd/sources/
        └── initial scaffold published automatically
        │
        ▼
artifactd stores an immutable version
        │
        ├── stable URL
        ├── registry metadata
        ├── version history
        └── library entry
        │
        ▼
http://artifacts.localhost/my-artifact
```

Publishing the same artifact again should update its existing entry and create a new version rather than overwrite history. Archived artifacts remain archived until explicitly unarchived.

## MVP

The first release should answer one question: **does it feel valuable when an agent-generated tool permanently exists in a local library?**

Current capabilities:

- a small `artifact.json` manifest
- standalone static artifacts
- dependency-free HTML, CSS, and JavaScript scaffolding via `artifact create --id <id>`
- `artifact create`, `publish`, `list`, `archive`, `unarchive`, `edit`, `versions`, and `restore` (`create` auto-publishes; `--open` launches the result)
- Go `artifactd` daemon with JSON-over-Unix-socket IPC
- SQLite-backed metadata registry
- filesystem-backed immutable versions
- artifact-specific localhost origins
- scoped workspace filesystem metadata
- live system metrics, agent-pushed JSON data, and transactional agent editing with automatic browser refresh
- a documented shell-first integration for agents

Future versions may add thumbnails, permanent artifact purge, and state-preserving HMR.

The MVP intentionally excludes AI chat, an IDE, arbitrary shell access, Node runtimes, cloud hosting, authentication, collaboration, and a plugin marketplace. See [MVP scope](docs/mvp.md).

## Documentation

- [Project vision](docs/vision.md) — the user problem, product philosophy, and intended experience
- [MVP scope](docs/mvp.md) — milestones, acceptance criteria, and explicit non-goals
- [Architecture](docs/architecture.md) — runtime boundaries and data flow
- [Protocol](docs/protocol.md) — CLI-to-daemon API
- [Artifact specification](docs/artifact-spec.md) — versioned artifact metadata and packaging
- [Artifact format](docs/artifact-format.md) — manifest and packaging conventions
- [Security model](docs/security.md) — threat model and constraints for generated content
- [Agent integration](docs/agent-integration.md) — shell-first integration and future directions
- [Default artifact](default/) — the built-in library starter/template artifact
- [Roadmap](docs/roadmap.md) — staged evolution beyond the MVP
- [Development](docs/development.md) — build and test commands
- [Artifact authoring](docs/authoring.md) — raw HTML, CSS, and JavaScript workflows
- [Contributing](CONTRIBUTING.md) — how to contribute while the project is still being shaped

These documents describe the current pre-alpha API and implementation boundaries. Runtime providers are intentionally narrow while the core interaction is validated.

## Quick start

Build the two native Linux binaries:

```bash
go build -o artifact ./cmd/artifact
go build -o artifactd ./cmd/artifactd
```

Start the daemon, create an artifact, open its initial preview, and list it:

```bash
./artifactd &
./artifact create --id demo --open
./artifact list
```

`artifact create` stores editable source outside the current repository, under
Artifactd's managed data directory. Use `--path ./demo --id demo` when a
repository-local source directory is intentional. Existing directory paths
remain supported by `publish`.

Create publishes the generated static files immediately. For an edit, begin a transaction before changing files and commit it when the complete source is ready:

```bash
source_dir="$(artifact create --id my-tool)"
artifact edit begin my-tool --message "Building the tool"
# edit files in "$source_dir"
artifact edit commit my-tool --session <session-id>
```

The browser shows a loading overlay during the transaction and refreshes after the validated immutable version is published.

Published artifacts are served at `http://<id>.artifacts.localhost:7337/`. The default Artifactd Home library is available at `http://artifacts.localhost:7337/`. Legacy path URLs remain available and redirect at the artifact root.

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
