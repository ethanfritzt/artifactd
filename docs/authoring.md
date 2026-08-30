# Artifact authoring

Artifactd's canonical authoring path is a dependency-free static web directory containing HTML, CSS, JavaScript, and relative assets. Artifactd supplies global library navigation at runtime for every non-home HTML artifact, so source files should not add a duplicate back-to-library control.

## Create an artifact

```bash
source_dir="$(artifact create --id my-tool)"
# create publishes the initial scaffold automatically
```

By default, `create` places the editable source in Artifactd's managed data
directory (`~/.local/share/artifactd/sources/my-tool`) rather than the current
repository. Use `artifact create --path ./my-tool --id my-tool` when local source
files are desired. Existing directory paths remain supported for publishing.

The generated directory contains:

```text
artifact.json
index.html
styles.css
app.js
```

Edit those files directly. Before a multi-file change, run `artifact edit begin my-tool --message "Updating the artifact"`; the browser shows a loading overlay while the session is active. Run `artifact edit commit my-tool --session <session-id>` when the complete source is ready. Failed validation leaves the previous immutable version served, and no dependency installation or build step is required.

## Static data and runtime providers

Additional JSON, SVG, and other regular assets can be committed beside the entry document. Artifacts may also read the narrow Artifactd runtime endpoints available from their artifact-specific origin, such as system metrics or workspace file metadata.

## Design guidance

Load the Artifactd design guidance when creating or redesigning an artifact. Start with the artifact's job, choose a contextual visual language, establish a clear hierarchy, and include responsive, accessible loading, empty, error, stale, and large-data states.

## Authoring guidelines

- Keep the published output self-contained and free of CDN dependencies.
- Begin an edit session for the directory that contains the validated `artifact.json` and entrypoint before making coordinated changes.
- Use semantic HTML and native browser controls where they fit the interaction.
- Match the visual representation to the meaning of the data.
- Keep runtime data reads read-only and agent-mediated writes outside the browser.
- Avoid exposing sensitive workspace data in generated snapshots.
