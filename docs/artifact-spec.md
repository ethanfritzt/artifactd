# Artifact specification

Artifactd is opinionated about the artifact contract, not the frontend stack used to author it. An artifact is a versioned, self-contained browser application described by a JSON specification and packaged as regular files.

## Specification

The current specification version is `1`:

```json
{
  "specVersion": 1,
  "artifact": {
    "id": "house-budget",
    "name": "House Budget",
    "description": "Explore home purchase scenarios"
  },
  "code": {
    "format": "files",
    "entry": "index.html",
    "stack": {
      "preset": "static-web",
      "target": "browser",
      "languages": ["html", "css", "javascript"],
      "build": {
        "type": "none",
        "output": "static"
      }
    }
  },
  "runtime": {
    "id": "web-static",
    "version": 1
  },
  "capabilities": []
}
```

`artifact.json` may also use the legacy flat form with `artifactVersion`, top-level metadata, `entry`, and a string `runtime`. Legacy `runtime: "web"` is normalized to `web-static` when read. New artifacts should use the structured form.

## Layers

### Artifact

The `artifact` block contains stable identity and user-facing metadata. The ID is the update identity: publishing the same ID creates a new immutable version.

### Code

The `code` block describes the executable files:

- `format` is currently `files`.
- `entry` is a normalized relative path to the browser entry document.
- `stack` describes how the files were authored and built.

The stack is descriptive authoring metadata. It does not install packages, execute commands, or grant host access.

The initial canonical stack is:

```text
HTML + CSS + plain JavaScript → web-static
```

Frameworks and UI kits may be represented as optional stack metadata. For example, the existing React authoring preset is React + Mantine + Vite producing static output. Its published runtime is still `web-static`.

### Runtime

The runtime identifies an Artifactd-controlled, allowlisted execution or serving implementation. The initial runtime is:

```text
web-static, version 1
```

Artifactd never treats a runtime value as an arbitrary shell command, package name, or server process.

### Capabilities

Capabilities will describe explicit access to Artifactd providers and mediated actions as those APIs are formalized. They are separate from the authoring stack and must not be inferred from framework or dependency metadata. The current runtime only accepts an empty capabilities list.

## Packaging

The specification is represented on disk as a directory:

```text
my-artifact/
├── artifact.json
├── index.html
├── styles.css
└── app.js
```

Additional regular static assets may be referenced by relative paths. Artifactd validates local paths, rejects symlinks and special files, and does not install dependencies or run authoring builds.

An agent-facing JSON envelope may eventually contain inline text files or references to assets. The directory package remains the canonical publish format for the initial runtime.

## Export and provenance

Artifactd guarantees that the published artifact can be served as static web files outside Artifactd. Framework-specific source export is only possible when the authoring source and a compatible adapter are available; Artifactd does not promise arbitrary conversion between frameworks.
