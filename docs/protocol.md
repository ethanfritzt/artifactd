# Local protocol

The `artifact` CLI communicates with `artifactd` using HTTP/JSON over a Unix domain socket. The control protocol is versioned under `/v1`. The socket is local-user-only and is not exposed by the browser HTTP listener.

## Health

```text
GET /v1/health
```

Response:

```json
{"status":"ok"}
```

## List artifacts

```text
GET /v1/artifacts[?include_archived=true]
```

Archived artifacts are omitted by default. Set `include_archived=true` to include them. Response artifacts include `workspace_id` when the current version belongs to a registered workspace and `archived_at` when archived.

## Publish

```text
POST /v1/artifacts/publish
Content-Type: multipart/form-data
```

Each artifact file is sent in a `file` part. The `X-Artifact-Path` part header carries the normalized relative path, such as `artifact.json` or `assets/chart.svg`; the filename remains the basename for multipart compatibility. The CLI also sends an internal `source_path` hint. The daemon associates a version with the longest matching registered workspace root only after resolving that directory and verifying that its complete file set and contents match the uploaded package. Missing, inaccessible, or mismatched hints publish without a workspace association; `source_path` is not trusted by itself.

Successful response:

```json
{
  "artifact": { "id": "house-budget", "name": "House Budget", "runtime": "web-static", "workspace_id": "vault", "current_version": 1 },
  "version": 1,
  "url": "http://house-budget.artifacts.localhost:7337/"
}
```

Publishing an existing ID creates the next immutable version and updates the current version. Publishing does not implicitly unarchive an artifact.

## Archive

Archive or unarchive an artifact without deleting its versions:

```text
POST   /v1/artifacts/{id}/archive
DELETE /v1/artifacts/{id}/archive
```

Archiving removes the artifact from the default list and Artifactd Home, preserves its stable URL and version history, and aborts any active editing session. The system artifact `artifactd-home` cannot be archived.

## Editing sessions

Editing is an explicit transaction over the Unix socket:

```text
POST   /v1/artifacts/{id}/edit
GET    /v1/artifacts/{id}/edit
POST   /v1/artifacts/{id}/edit/progress
POST   /v1/artifacts/{id}/edit/commit
DELETE /v1/artifacts/{id}/edit?session_id=...
```

Begin request:

```json
{"directory":"/home/user/project/demo","message":"Redesigning the dashboard"}
```

The directory must contain a valid artifact with the requested stable ID and an existing published version. The daemon returns an opaque session ID and the immutable base version:

```json
{"session_id":"edit-...","artifact_id":"demo","base_version":66,"status":"editing","expires_at":"..."}
```

The agent may edit the source directory normally. Those intermediate files are never served. Progress messages renew the session lease and are presented by the browser overlay. Commit is a multipart request containing `session_id`, `base_version`, `source_path`, and the complete artifact file set. The daemon validates the complete package and publishes exactly one immutable version only if the base version is still current.

The browser-only endpoint emits Server-Sent Events:

```text
GET /_artifactd/events
GET /_artifactd/edit
```

Events have the form:

```json
{"type":"edit_started","artifact_id":"demo","session_id":"edit-...","message":"..."}
{"type":"edit_progress","artifact_id":"demo","session_id":"edit-...","message":"..."}
{"type":"edit_committing","artifact_id":"demo","session_id":"edit-...","message":"Publishing artifact…"}
{"type":"artifact_changed","artifact_id":"demo","session_id":"edit-..."}
{"type":"edit_error","artifact_id":"demo","session_id":"edit-...","error":"..."}
{"type":"edit_aborted","artifact_id":"demo","session_id":"edit-..."}
{"type":"edit_expired","artifact_id":"demo","session_id":"edit-..."}
```

The injected browser client displays the old published version under a loading overlay during the session and reloads once after a successful commit. Invalid packages leave the previous published version unchanged. Sessions are in-memory, expire after inactivity, and end when aborted, committed, or the daemon shuts down.

## Versions and restore

Durable versions can be listed and restored over the Unix socket:

```text
GET  /v1/artifacts/{id}/versions
POST /v1/artifacts/{id}/restore
```

Restore request:

```json
{"version":1}
```

Restoring changes the durable current-version pointer and aborts any active editing session. It never copies files back into the agent's source workspace.

## Workspaces

Register a workspace once:

```text
POST /v1/workspaces
Content-Type: application/json
```

Request:

```json
{"id":"vault","root":"/home/user/vault"}
```

List workspaces:

```text
GET /v1/workspaces
```

Workspace roots are canonical existing directories. The daemon matches a verified published source directory to the most-specific registered root; a client-supplied `source_path` alone never grants a workspace association.

## Agent runtime data

Pi or another local agent can push structured JSON obtained from CLI or MCP tools:

```text
POST /v1/artifacts/{id}/data/{source}
Content-Type: application/json
```

The source is a lowercase identifier and the body is limited to 5 MiB. Runtime data is held in memory and is separate from immutable artifact versions.

## Browser runtime endpoints

Artifacts are served on an artifact-specific localhost origin:

```text
http://<id>.artifacts.localhost:7337/
```

The browser listener exposes read-only runtime data for that artifact:

```text
GET /_artifactd/library
GET /_artifactd/system
GET /_artifactd/files?path=...&depth=...
GET /_artifactd/data/{source}
GET /_artifactd/edit
GET /_artifactd/events
```

`/_artifactd/library` returns the current artifact metadata from SQLite. `/_artifactd/system` returns CPU, memory, load, and process data. `/_artifactd/files` returns metadata scoped to the artifact's registered workspace. `/_artifactd/edit` returns the active editing session, and `/_artifactd/events` emits editing transitions over Server-Sent Events. The legacy path URL remains available for static content and redirects its artifact root to the artifact-specific origin.

## Errors

Errors use a stable JSON shape:

```json
{"error":"human-readable error"}
```

The CLI converts transport and daemon errors into non-zero Unix exit codes.
