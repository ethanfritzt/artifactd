# Artifact specification

Artifactd stores versioned, self-contained browser applications described by a JSON specification and packaged as regular files.

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
    "entry": "index.html"
  },
  "runtime": {
    "id": "web-static",
    "version": 1
  },
  "capabilities": []
}
```

`artifact.json` may also use the legacy flat form with `artifactVersion`, top-level metadata, `entry`, and a string `runtime`. Legacy `runtime: "web"` is normalized to `web-static` when read. New artifacts should use the structured form above.

## Layers

### Artifact

The `artifact` block contains stable identity and user-facing metadata. The ID is the update identity: publishing the same ID creates a new immutable version.

### Code

The `code` block describes the packaged files:

- `format` is currently `files`.
- `entry` is a normalized relative path to the browser entry document.

The canonical authoring form is dependency-free HTML, CSS, JavaScript, Markdown, and relative static assets. Artifactd serves regular assets directly and renders `.md` and `.markdown` documents to HTML without running a build or package runtime. Raw HTML in Markdown is disabled.

### Runtime

The runtime identifies an Artifactd-controlled, allowlisted serving implementation. The initial runtime is:

```text
web-static, version 1
```

Artifactd never treats a runtime value as an arbitrary shell command, package name, or server process.

### Capabilities

Capabilities will describe explicit access to Artifactd providers and mediated actions as those APIs are formalized. The current runtime only accepts an empty capabilities list.

## Packaging

The specification is represented on disk as a directory:

```text
my-artifact/
├── artifact.json
├── index.html
├── styles.css
└── app.js
```

Additional regular static assets may be referenced by relative paths. Markdown entrypoints can link to other packaged Markdown files and same-origin images with relative URLs; directly requested Markdown files are rendered with the same daemon-owned stylesheet. Artifactd validates local paths, rejects symlinks and special files, and does not install dependencies or run authoring builds.

An agent-facing JSON envelope may eventually contain inline text files or references to assets. The directory package remains the canonical publish format for the initial runtime.

## Export

HTML-entry artifacts are standalone web applications and can be served as static web files outside Artifactd. Markdown-entry packages remain portable source documents, but another static host needs its own Markdown renderer or a pre-rendered HTML export to reproduce Artifactd's browser presentation. Neither form depends on a frontend framework or package runtime.
