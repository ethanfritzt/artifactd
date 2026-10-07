# Artifact authoring

Artifactd's canonical authoring path is a dependency-free static web directory containing HTML, CSS, JavaScript, Markdown, and relative assets. Artifactd supplies global library navigation at runtime for every non-home HTML or Markdown artifact, so source files should not add a duplicate back-to-library control.

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

## Markdown

Use a Markdown file as `code.entry` when an artifact is primarily a document:

```json
{
  "specVersion": 1,
  "artifact": {"id": "project-notes", "name": "Project Notes"},
  "code": {"format": "files", "entry": "README.md"},
  "runtime": {"id": "web-static", "version": 1}
}
```

Artifactd renders `.md` and `.markdown` files with GitHub-flavored tables, task lists, fenced code blocks, autolinks, and strikethrough. Raw HTML is disabled. Relative links to packaged Markdown documents and same-origin images retain their normal browser behavior.

For a file that should not become a durable library artifact, create an in-memory preview that lasts up to one hour:

```bash
artifact preview ./README.md --open
```

The preview uploads only the selected Markdown file, does not read or publish its parent directory, does not appear in the artifact library, and expires automatically. It opens in Preview with one Edit/Preview toggle (Ctrl/Cmd+E), explicit Save, browser Print/Save as PDF, and DOCX export. Reading and editing share a flat, responsive note workspace with matching typography; there are no split panes or width settings. The locally bundled Markdown source editor includes styled headings, syntax highlighting, undo/redo, indentation, search (Ctrl/Cmd+F), bold/italic shortcuts (Ctrl/Cmd+B/I), and Ctrl/Cmd+S, without a formatting toolbar or line-number gutter. Draft previews update without saving; switching views keeps your edits, and saving preserves your editing position without reloading the page. Changes remain in the current browser page until you explicitly save; leaving with unsaved changes warns before navigation. Both PDF and DOCX export the saved version, not the draft. Saving is limited to that file, uses a scoped in-memory capability, writes atomically, and rejects external changes rather than overwriting them. DOCX preserves text and block order with basic paragraph/list treatment; rich styling, table layout, and embedded or relative assets are not preserved.

On Linux, `artifact integration install` registers **Artifactd Markdown Preview** in the **Open With** menu used by GNOME Files and other MIME-aware file managers. It does not change the user's default Markdown application.

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
