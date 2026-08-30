# Phase 1 MVP scope

This document describes the static baseline that preceded the workspace runtime extension.

## Goal

An agent or human can create a standalone artifact, publish it through a local daemon, list published artifacts from the CLI, and reopen one from a stable local URL.

## In scope

### Commands

```text
artifact create [artifact-id]
artifact publish <directory-or-id> [--open]
artifact list [--include-archived] [--output table|json]
artifact archive <artifact-id>
artifact unarchive <artifact-id>
```

### Artifact input

- a directory containing `artifact.json` and a static entry file
- HTML, CSS, JavaScript, SVG, and other regular static assets
- no dependency installation or build step required by the daemon; optional authoring toolchains must publish static output
- stable ID in the manifest

### Runtime and storage

- Go `artifactd` daemon
- JSON-over-Unix-socket CLI protocol
- static localhost HTTP server
- SQLite metadata registry
- filesystem-backed immutable versions
- XDG-compatible user data and runtime directories
- default browser URLs under `<id>.artifacts.localhost:7337` (with legacy path compatibility)

### Lifecycle

- publish a new artifact
- publish the same ID as a new version
- list current artifact metadata
- preserve earlier versions for future restore functionality
- archive and unarchive artifacts without deleting versions
- clean daemon shutdown and restart persistence

## Out of scope

The Phase 1 MVP will not include:

- rich library UI, thumbnails, and search
- thumbnails
- permanent artifact deletion or purge commands
- AI chat or agent orchestration
- IDE or code editor
- Node or other runtime dependencies inside the daemon or published artifact
- arbitrary backend processes
- Docker-based runtime isolation
- cloud hosting, accounts, or authentication
- collaboration or sharing
- databases exposed directly to artifacts
- unrestricted filesystem, network, or shell access
- plugin marketplace
- artifact composition or branching
- mobile applications

## Acceptance criteria

The MVP is ready for dogfooding when a user can:

1. Start `artifactd`.
2. Run `artifact create --id demo` and receive an automatically published scaffold.
3. Edit the generated static files in the returned managed source directory.
4. Optionally pass `--open` to launch the initial URL in the default browser.
5. Run `artifact edit begin demo`, edit the source, and commit with `artifact edit commit demo --session <session-id>`; the browser shows the editing overlay and refreshes after publication.
6. Confirm a failed commit leaves the previous immutable version served.
7. Run `artifact list` and see the published artifact.
8. Publish a changed directory with the same ID and create a new version.
9. Archive an artifact, confirm it is hidden from the active library, then unarchive it.
10. Restart `artifactd` and retain the registry and published content.
11. Confirm unsafe paths, symlinks, and special files are rejected.

## Deployment

The primary deployment is native Go binaries on Linux. A systemd user service is a follow-up to foreground daemon operation. Docker is optional for development or CI and is not required by the MVP.
