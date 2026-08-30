# Transactional Artifact Editing

This document supersedes the former filesystem-watcher live-preview plan.

## Decision

Artifactd uses explicit editing sessions rather than trying to infer agent
lifecycle from filesystem events.

```text
begin → ordinary source edits → commit or abort
```

The browser continues to serve the last immutable version during editing. A
successful commit uploads the complete source tree, validates it, installs one
new immutable version, advances the current-version pointer, and emits a
browser reload event.

## Session protocol

```text
POST   /v1/artifacts/{id}/edit
GET    /v1/artifacts/{id}/edit
POST   /v1/artifacts/{id}/edit/progress
POST   /v1/artifacts/{id}/edit/commit
DELETE /v1/artifacts/{id}/edit?session_id=...
```

A session owns one artifact and records its canonical source directory, base
version, opaque session ID, status, message, and inactivity lease. Only one
session may edit an artifact at a time. A commit is accepted only when its
session and base version still match. The registry performs the version check
inside the SQLite publish transaction, preventing a stale session from
overwriting a concurrent publish.

Sessions are daemon-memory state. They expire safely after inactivity and are
cleared when the daemon shuts down. Expiration, abort, validation failure, and
commit completion never change the published version unless commit succeeds.

## Browser behavior

Artifact HTML receives a daemon-owned edit client and overlay. The client
connects to `/_artifactd/events` and obtains the current state from
`/_artifactd/edit`, so a page opened during an edit displays the correct
status. It shows the agent's message while editing, keeps the old page covered
while committing, and reloads once after `artifact_changed`.

This is post-write static preview, not generic HMR. Artifactd does not execute
build tools, inspect JavaScript state, serve source directories, or infer edit
boundaries.

## Removed behavior

The former `artifact watch`, `artifact unwatch`, and `artifact live` commands,
recursive `fsnotify` watching, debounce logic, and temporary live snapshots are
removed. `artifact publish` remains available for direct non-session publishing
and all publication still uses staging, validation, atomic installation, and
immutable version history.

## Agent integration

The Artifactd skill instructs agents to begin before coordinated file writes,
optionally send progress messages, commit after all writes finish, and abort
abandoned work. The protocol remains usable by any shell-capable agent or
human; it does not require an agent-specific SDK or completion signal.
