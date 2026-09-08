# Roadmap

The local static-artifact MVP is implemented. Future work should be driven by
dogfooding the publish–reopen and agent-editing loops rather than by adding
platform complexity prematurely.

## Current baseline

- static artifact validation and publishing
- local SQLite registry and immutable filesystem versions
- JSON-over-Unix-socket CLI protocol
- foreground daemon with separate control and browser listeners
- artifact-specific localhost origins and legacy path compatibility
- named workspaces with read-only filesystem metadata
- system metrics and agent-pushed JSON runtime data
- transactional source editing with browser status events and refresh after commit
- dependency-free static authoring scaffold
- server-rendered Markdown artifacts and temporary Markdown previews
- Linux Markdown MIME integration for GNOME Files and other file managers
- Artifactd Home library artifact
- CLI version listing, restore, archive, and unarchive operations

## Next priorities

- dogfood with Pi and other shell-capable agents
- improve the Artifactd Home library UI
- expose version browsing and restore in the library
- add visual cards, thumbnails, search, and pinning
- add provenance and predictable update confirmation
- improve setup and supervised deployment ergonomics

## Later possibilities

- persistent artifact state separate from code
- mediated file writes and authenticated actions
- agent callbacks and structured action requests
- richer static authoring helpers
- state-preserving HMR with an explicit runtime contract
- typed artifact outputs and composition
- fork/eject workflows
- optional sharing and remote runtimes
- broader desktop integration and an open artifact protocol specification

## Explicitly not a near-term goal

The project should not become an IDE, general deployment platform, cloud
service, or unrestricted execution environment before the local artifact loop
proves its value.
