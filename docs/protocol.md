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

Each artifact file is sent in a `file` part. The `X-Artifact-Path` part header carries the normalized relative path, such as `artifact.json` or `assets/chart.svg`; the filename remains the basename for multipart compatibility. The CLI also sends an internal `source_path` field so the daemon can associate the version with the longest matching registered workspace root.

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

Archiving removes the artifact from the default list and Artifactd Home, preserves its stable URL and version history, and stops any active live preview. The system artifact `artifactd-home` cannot be archived.

## Live preview

Start, inspect, and stop an ephemeral live preview over the Unix socket:

```text
POST   /v1/artifacts/{id}/watch
GET    /v1/artifacts/{id}/live
DELETE /v1/artifacts/{id}/watch
```

Start request:

```json
{"directory":"/home/user/project/demo"}
```

The watched directory must contain a valid artifact with the requested stable ID and must already have a published version. The daemon validates and snapshots the directory without changing the registry version. A successful response includes the live status, content hash, and stable URL.

While a live session is active, the browser origin serves the latest validated live snapshot. The browser-only endpoint below emits Server-Sent Events; the injected live client reloads the page after an `artifact_changed` event:

```text
GET /_artifactd/events
```

Event data has the form:

```json
{"type":"artifact_changed","artifact_id":"demo","hash":"..."}
{"type":"artifact_error","artifact_id":"demo","message":"..."}
```

Invalid edits do not replace the previous valid snapshot. Live sessions are in-memory and end when explicitly stopped, the watch lease expires, or the daemon shuts down.

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

Restoring changes the durable current-version pointer and stops any active live session. It never copies files back into the agent's source workspace.

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

Workspace roots are canonical existing directories. The daemon matches published source paths to the most-specific registered root.

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
```

`/_artifactd/library` returns the current artifact metadata from SQLite. `/_artifactd/system` returns CPU, memory, load, and process data. `/_artifactd/files` returns metadata scoped to the artifact's registered workspace. The legacy path URL remains available for static content and redirects its artifact root to the artifact-specific origin.

## Errors

Errors use a stable JSON shape:

```json
{"error":"human-readable error"}
```

The CLI converts transport and daemon errors into non-zero Unix exit codes.
