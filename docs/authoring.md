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

Edit those files directly. Run `artifact watch my-tool` to keep the artifact URL open during edits; the browser shows a blurred loading overlay while a valid snapshot is being assembled. Run `artifact publish my-tool` when you want an explicit immutable checkpoint. No dependency installation or build step is required.

## Static data and runtime providers

Additional JSON, SVG, and other regular assets can be committed beside the entry document. Artifacts may also read the narrow Artifactd runtime endpoints available from their artifact-specific origin, such as system metrics or workspace file metadata.

## Design guidance

Load the Artifactd design guidance when creating or redesigning an artifact. Start with the artifact's job, choose a contextual visual language, establish a clear hierarchy, and include responsive, accessible loading, empty, error, stale, and large-data states.

## Authoring guidelines

- Keep the published output self-contained and free of CDN dependencies.
- Watch the directory that contains the validated `artifact.json` and entrypoint.
- Use semantic HTML and native browser controls where they fit the interaction.
- Match the visual representation to the meaning of the data.
- Keep runtime data reads read-only and agent-mediated writes outside the browser.
- Avoid exposing sensitive workspace data in generated snapshots.
