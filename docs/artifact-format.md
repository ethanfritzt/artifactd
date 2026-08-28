# Artifact format

Published artifacts are standalone web applications with no runtime installation or server process. Artifactd is opinionated about the artifact contract, not the frontend framework used to author it.

The versioned specification is documented in [Artifact specification](artifact-spec.md). The package remains a directory of regular files with an `artifact.json` specification:

```text
my-artifact/
├── artifact.json
├── index.html
├── styles.css
└── app.js
```

Additional regular static assets may be referenced using relative paths.

## Runtime contract

The initial runtime is the allowlisted `web-static` runtime. It serves the published entry document and its relative assets. Authors may use plain HTML/CSS/JavaScript, TypeScript, React, Svelte, Vue, or another toolchain, provided the resulting output is static browser content.

The default authoring stack is:

```text
HTML + CSS + plain JavaScript
```

The existing React/Vite/Mantine scaffold is an optional authoring preset. Mantine is not required by the runtime and does not define the visual language of artifacts.

When serving a non-home HTML entrypoint, Artifactd adds platform navigation linking back to the Artifactd Home library. This navigation is response-level platform chrome: it is not written into the artifact source or immutable published files, and authoring stacks should not add a duplicate library link.

## Packaging rules

- directories are published, not archives
- paths must be local and remain inside the published directory
- symlinks and special files are rejected
- the entrypoint must be a regular file
- artifacts must be complete and self-contained
- publishing never mutates the source directory
- generated versions are immutable
- total artifact size and file count are bounded

## Storage

The daemon stores the published copy under its data directory. Source paths are not authoritative and are not required for future updates.

## Runtime data

Artifacts may use Artifactd runtime endpoints when served from an artifact-specific origin. The available data depends on the artifact's registered workspace and local providers. Runtime capabilities are separate from the code stack and are not granted by framework or dependency metadata.

## Compatibility

Legacy manifests using `artifactVersion`, flat metadata, top-level `entry`, and `runtime: "web"` remain readable. New artifacts should use the structured specification and `runtime: {"id":"web-static","version":1}`.
