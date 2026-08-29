# Live Artifact Editing Implementation Plan

## Goal

An agent creates an artifact with Pi, returns a stable local URL, and can continue editing the artifact through Pi while the already-open browser view updates automatically. The normal loop should be:

```text
Pi edits source files → artifactd detects a change → browser refreshes
                                      ↓
                          optional durable checkpoint
```

This should feel like a local hot refresh, not a deployment workflow.

## Product model

Keep three concepts separate:

- **Source workspace:** files that Pi edits. This is the agent's authoritative working copy.
- **Live preview:** a validated, temporary snapshot served at the stable artifact URL while watch mode is active.
- **Published version:** an immutable, durable checkpoint created explicitly with `artifact publish`.

A file save must not automatically create a registry version. Otherwise normal editing would create excessive versions and make rollback/history noisy.

The first implementation should provide browser reload rather than module-level HMR. While the snapshot is being assembled, the browser keeps the last valid page behind a blurred loading overlay. Reloading is sufficient for standalone HTML, CSS, and JavaScript artifacts.

## Current gaps

The current implementation already provides useful foundations:

- workspace registration and workspace association during publish
- immutable filesystem-backed versions
- artifact-specific browser origins
- separate Unix-socket control and browser listeners
- runtime data push infrastructure

It currently does not provide:

- a persisted or active source-directory binding
- filesystem watching
- live snapshots distinct from published versions
- browser change notifications
- version restore

The live feature must not serve a workspace directory directly. Direct serving could expose unrelated files and could display partially-written files while Pi or a build tool is updating them.

## Proposed user workflow

Initial API recommendation:

```bash
artifact create --id demo       # publishes the initial scaffold
artifact watch demo              # starts live preview for this artifact
```

The command should print the stable URL and remain usable as a foreground process. A later convenience form may combine the operations:

```bash
artifact publish demo --watch
```

The artifact ID comes from `artifact.json`. New scaffolds are stored under Artifactd's managed `sources/<artifact-id>/` directory, outside the current repository. Use `--path` with `artifact create` when repository-local source is intentional. Watch mode is explicitly opt-in so the daemon does not unexpectedly retain paths, consume resources, or follow files after an agent exits.

Pi needs no special editing protocol: it continues to modify files through its normal shell/editor tools. The shipped skill should teach Pi to start watch mode once, keep the URL open, and use ordinary edits for subsequent iterations.

## Architecture

### 1. Live session manager

Add a daemon-owned live session manager with one session per artifact ID. A session contains:

- artifact ID
- canonical watched directory
- current live snapshot path
- last accepted content hash
- current status (`watching`, `building`, `ready`, or `error`)
- last error, if any
- cancellation function and watcher lifecycle

The source directory must be validated as an existing directory and associated with the artifact's manifest ID. The daemon should reject a watch request when the directory's manifest belongs to another artifact.

The session manager should be in-memory initially. Live sessions are ephemeral and should stop when the watch process disconnects or the daemon shuts down. Published versions remain durable in SQLite and on disk.

### 2. Filesystem watching

Use a Linux-compatible filesystem watcher, such as `fsnotify`, and watch the artifact source tree recursively. Handle create, write, remove, rename, and directory events. Re-register directories after directory rename/create events where necessary.

On an event:

1. debounce events for a short interval
2. wait until the source is readable and stable
3. copy the complete source tree into a new staging directory
4. reject symlinks, special files, traversal, oversized files, and invalid manifests using the existing validation rules
5. atomically make the new validated snapshot live
6. notify connected browsers

The watcher must never update the currently served snapshot in place. A failed or incomplete edit must leave the last valid preview available.

### 3. Live snapshot storage

Add a storage area separate from immutable versions, for example:

```text
~/.local/share/artifactd/
└── live/
    └── <artifact-id>/
        └── snapshots/<temporary-snapshot>
```

The snapshot manager should publish by directory swap or an equivalent atomic pointer update. It should clean up old snapshots and all abandoned staging directories on session stop and daemon startup.

Live snapshots do not increment `current_version`, modify the versions table, or change published history.

### 4. Browser serving

When a live session is active, the artifact-specific URL should serve the current validated live snapshot. When no session is active, it should continue serving the current immutable published version.

Add a browser-only event endpoint, for example:

```text
GET /_artifactd/events
```

The endpoint should use Server-Sent Events because the initial requirement is one-way daemon-to-browser notification. Events should include:

```json
{"type":"artifact_changed","artifact_id":"demo","hash":"..."}
{"type":"artifact_error","artifact_id":"demo","message":"..."}
```

The artifact page can connect with `EventSource` and call `location.reload()` after `artifact_changed`. The existing artifact-specific origin and `connect-src 'self'` CSP should keep this scoped to the current artifact origin.

The event endpoint should support client disconnects, bounded subscriber counts, keepalive comments, and clean shutdown. It must never become a control-plane endpoint.

### 5. Control protocol and CLI

Add control-plane operations over the existing Unix socket:

```text
POST /v1/artifacts/{id}/watch
DELETE /v1/artifacts/{id}/watch
GET /v1/artifacts/{id}/live
```

Suggested watch request:

```json
{"directory":"/home/user/project/demo"}
```

Suggested live response:

```json
{
  "artifact_id":"demo",
  "directory":"/home/user/project/demo",
  "status":"ready",
  "hash":"...",
  "error":""
}
```

Add corresponding CLI commands:

```text
artifact watch <directory-or-id>
artifact unwatch <artifact-id>
artifact live <artifact-id>
```

`artifact watch` should start a long-lived session and return a nonzero exit code if the initial snapshot cannot be validated. Runtime changes should be reported to stderr while the stable URL remains on stdout or in the initial response, so scripts can consume it reliably.

The daemon should treat the watch request as local control-plane access only. The public browser listener must not accept watch, unwatch, publish, or restore requests.

### 6. Durable checkpoints and rollback

Keep `artifact publish <directory-or-id>` as the explicit checkpoint operation. It should continue creating an immutable version even when live watch mode is active.

Add version inspection and restore separately from the initial hot-refresh milestone:

```text
artifact versions <artifact-id>
artifact restore <artifact-id> <version>
```

Restoring should update the durable current-version pointer and either:

- stop the live session, or
- require the user to explicitly resume watch mode.

Do not silently copy a restored version back into the agent's source workspace. That would mutate files owned by Pi and could destroy uncommitted work.

## Implementation sequence

### Phase 1: Live preview and reload

1. Define live-session lifecycle and the behavior of `watch`, `unwatch`, and daemon shutdown.
2. Add live snapshot storage and atomic snapshot swapping.
3. Add recursive file watching with debounce and validation.
4. Add the in-memory live session manager to the daemon.
5. Make the browser server select a live snapshot when one is active.
6. Add SSE subscriptions and browser reload notifications.
7. Add CLI/client/protocol support for `artifact watch` and `artifact live`.
8. Update the agent skill and authoring documentation with the edit loop.

### Phase 2: Checkpoints and rollback

1. Add version listing and restore APIs.
2. Define behavior when restore, publish, and watch operate concurrently.
3. Add explicit checkpoint guidance for Pi.
4. Add cleanup and retention rules for live snapshots.

### Phase 3: Optional richer development experience

Only after reload-based preview is reliable:

- preserve selected browser state across refreshes where practical
- support build commands through an explicitly managed authoring process, without giving published artifacts arbitrary shell access
- investigate state-preserving live editing for future authoring helpers
- add structured agent actions from the artifact UI as a separate capability

## Concurrency and failure rules

- Multiple watch requests for the same artifact should either replace the existing session explicitly or return a conflict; they must not race.
- A publish and live snapshot may happen concurrently, but each must use its own staging directory.
- A snapshot is visible only after complete validation and atomic swap.
- If Pi writes an invalid manifest or incomplete build, keep serving the previous valid snapshot and emit an error event.
- If the source directory disappears, mark the session as errored and keep the last valid snapshot available until unwatch or session cleanup.
- Browser reloads may lose transient UI state. Artifacts can use browser storage or explicit state persistence if needed.

## Security requirements

- Watch requests remain on the user-only Unix socket.
- Canonicalize and validate the watched directory before starting a session.
- Do not serve the source directory directly.
- Apply the existing symlink, special-file, path, size, and manifest validation rules to every snapshot.
- Never allow the live route to escape the artifact snapshot root.
- Do not add shell execution, process control, package installation, or write access to the browser runtime.
- Bound watcher count, file count, snapshot size, event subscribers, and event message sizes.
- Ensure an untrusted artifact cannot subscribe to another artifact's events through host or path manipulation.

## Testing plan

### Unit tests

- watcher debounce behavior
- create/write/remove/rename event handling
- manifest and file validation failures
- atomic snapshot replacement
- stale snapshot cleanup
- session start, replacement, cancellation, and shutdown
- event subscription, keepalive, disconnect, and bounded fan-out
- live-versus-published serving selection

### Integration tests

- start daemon, publish an artifact, start watch mode, and fetch its stable URL
- modify `index.html` and observe the new content without republishing
- modify a CSS/JS asset and observe an `artifact_changed` event
- write an invalid artifact and confirm the previous snapshot remains served
- publish a checkpoint while watch mode is active
- stop watching and confirm the published version remains available
- restart the daemon and confirm live sessions are ephemeral while published versions persist
- verify browser endpoints cannot invoke watch or publish operations

### Acceptance criteria

The feature is ready when a user can:

1. Create and publish an artifact once.
2. Start watch mode and open its stable URL.
3. Ask Pi to edit the artifact's source files.
4. See the browser update automatically without running publish again.
5. Observe the last valid artifact when an edit is temporarily invalid.
6. Explicitly publish a durable checkpoint when the result is worth preserving.
7. Stop watch mode without losing the latest published version.
8. Confirm no unrelated workspace files are exposed through live serving.

## Non-goals

This feature does not initially provide:

- browser-side arbitrary agent commands
- collaborative multi-user editing
- conflict-free replicated editing (OT/CRDT)
- automatic durable version creation on every save
- a Node server or package runtime inside artifactd
- state-preserving live editing
- remote or cloud deployment
