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

Publishing an existing ID creates the next immutable version and updates the current version. Publishing does not implicitly unarchive an artifact. Publish requests are limited to 50 MiB and 1,000 regular files; invalid manifests, symlinks, special files, and unsafe paths are rejected.

## Create a Markdown preview

```text
POST /v1/previews
Content-Type: multipart/form-data
```

The request contains exactly one `.md` or `.markdown` file part (limited to 5 MiB) and an absolute `source_path` field. The daemon retains the selected path, content hash, and a random save capability only in bounded memory; it does not collect sibling files or disclose the path in the response. A preview is excluded from the artifact registry and expires after at most one hour. Legacy clients may omit `source_path` and receive a read-only preview.

Successful response:

```json
{
  "id": "preview-0123456789abcdef0123456789abcdef",
  "name": "README.md",
  "url": "http://preview-0123456789abcdef0123456789abcdef.artifacts.localhost:7337/",
  "expires_at": "2026-03-18T13:00:00Z"
}
```

The preview URL is browser-readable, but preview creation remains available only through the user-restricted Unix socket. Editable previews expose a same-origin `POST /_artifactd/save` endpoint guarded by the preview's bearer capability and exact Origin check; the endpoint writes only the selected file and returns `409 Conflict` if its content changed externally. Read-only previews do not expose saving. `GET /_artifactd/document.docx` creates a DOCX download from the current saved preview content (text and block order, with basic paragraph/list treatment; rich styling, table layout, and assets are not preserved). PDF export uses the browser print dialog.

## Get an artifact

Fetch the current artifact metadata and version:

```text
GET /v1/artifacts/{id}
```

The response has the same artifact and version fields as the publish response,
plus the current entry path:

```json
{
  "artifact": {"id":"demo","current_version":2},
  "version":2,
  "entry":"index.html"
}
```

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

The directory must contain a valid artifact with the requested stable ID and an existing published version. Keep the manifest ID unchanged until commit. The daemon returns an opaque session ID and the immutable base version:

```json
{"session_id":"edit-...","artifact_id":"demo","base_version":66,"status":"editing","expires_at":"..."}
```

The agent may edit the source directory normally. Those intermediate files are never served. Progress messages renew the five-minute inactivity lease and are presented by the browser overlay; long-running edits must send progress before the lease expires. Commit is a multipart request containing `session_id`, `base_version`, `source_path`, and the complete artifact file set. The daemon validates the complete package and publishes exactly one immutable version only if the base version is still current. The source path must match the session's canonical directory; it is not trusted as a workspace claim.

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

The injected browser client displays the old published version under a loading overlay during the session and reloads once after a successful commit. Invalid packages leave the previous published version unchanged. Sessions are in-memory, expire after five minutes without activity, and end when aborted, committed, or the daemon shuts down. Begin and commit return `201 Created`; progress and status return `200 OK`; abort returns `204 No Content`. A stale base version or concurrent edit returns `409 Conflict`.

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
POST /_artifactd/save             (editable temporary Markdown preview only)
POST /_artifactd/render           (editable temporary Markdown draft rendering only)
GET  /_artifactd/document.docx    (temporary Markdown preview only)
```

`/_artifactd/library` returns the current artifact metadata from SQLite. `/_artifactd/system` returns CPU, memory, load, and process data. `/_artifactd/files` returns metadata scoped to the artifact's registered workspace. `/_artifactd/edit` returns the active editing session, and `/_artifactd/events` emits editing transitions over Server-Sent Events. The legacy path URL remains available for static content and redirects its artifact root to the artifact-specific origin.

`/_artifactd/render` and `/_artifactd/save` require the preview's bearer save
capability and an exact same-origin `Origin` header. Both accept a single
`{"content":"Markdown source"}` JSON object with a decoded-source limit of
5 MiB and a bounded JSON envelope. Render returns `{"html":"safe HTML fragment"}`
without changing the file or saved preview. Save atomically writes the selected
file and returns `{"saved":true,"html":"safe HTML fragment"}`, allowing the
browser to update its saved baseline without a page reload. A source-file
conflict returns `409` and leaves the browser draft intact. PDF and DOCX exports
continue to use the saved version.

## Errors

Errors use a stable JSON shape:

```json
{"error":"human-readable error"}
```

The CLI converts transport and daemon errors into non-zero Unix exit codes. Requests use the stable `{"error":"..."}` JSON shape; a missing artifact or edit session is `404 Not Found`, malformed input is `400 Bad Request`, and oversized publish or commit uploads are `413 Request Entity Too Large`.
