# Roadmap

This roadmap is directional. The project should advance based on whether the core publish-and-reopen experience is useful, not on completing every possible platform feature.

## Phase 0 — Validate the model

- documentation-first project definition
- settle the smallest manifest and lifecycle vocabulary
- collect example artifacts and dogfooding scenarios
- identify the browser isolation model before implementation

## Phase 1 — MVP runtime

- static artifact validation and publishing
- `artifact create`, `artifact publish`, and `artifact list`
- local registry and immutable versions
- JSON-over-Unix-socket CLI protocol
- stable path-based URLs with artifact-specific runtime origins
- foreground daemon with separate control and browser listeners
- terminal-friendly and JSON output
- native Linux binaries

## Phase 2 — Workspace runtime

- named workspace roots and artifact associations
- artifact-specific localhost origins
- read-only system and filesystem providers
- live runtime data channels
- agent-pushed JSON snapshots and updates
- opt-in watched source directories with validated live preview snapshots and browser refresh events

## Phase 3 — Artifact authoring and library

- versioned artifact specification with source stack and runtime metadata
- dependency-free static web authoring scaffold as the default
- optional React/Vite/Mantine authoring preset
- opt-in Cytoscape graph feature
- reproducible static frontend builds
- design guidance and reliable artifact states
- `artifacts.localhost` library
- visual cards and metadata
- search and pinning
- thumbnails
- version browsing and restore actions (CLI baseline is available; library UI remains future work)
- polished empty, error, and loading states

## Phase 4 — Agent integrations

- Pi skill/instructions
- examples for other shell-capable agents
- provenance fields where useful
- predictable update behavior

## Phase 5 — Actions and state

- persistent artifact state separate from code
- mediated file writes and authenticated actions
- agent callbacks and structured action requests
- stronger trust handling for imported artifacts
- validated SDK surface

## Later possibilities

- Svelte or another constrained source runtime
- agent bridge and interactive actions
- typed artifact outputs and composition
- provenance and semantic search
- desktop `.desktop` entries
- state-preserving HMR for optional React/Vite projects
- fork/eject workflows
- optional sharing and remote runtimes
- open artifact protocol specification

## Explicitly not a near-term goal

The project should not become an IDE, general deployment platform, cloud service, or unrestricted execution environment before the local static-artifact loop proves its value.
