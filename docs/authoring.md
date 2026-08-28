# Artifact authoring

Artifactd accepts static artifact output regardless of the authoring tools used to create it. The canonical artifact target is the browser platform: HTML, CSS, JavaScript, and relative assets served by the `web-static` runtime. Artifactd supplies global library navigation at runtime for every non-home HTML artifact, so authoring projects should not implement a stack-specific back-to-library control.

## Static web template

The default template is dependency-free:

```bash
source_dir="$(artifact create --id my-tool)"
artifact publish my-tool
```

By default, `create` places the editable source in Artifactd's managed data
directory (`~/.local/share/artifactd/sources/my-tool`) rather than the current
repository. Use `artifact create --path ./my-tool --id my-tool` when local source
files are desired. Existing directory paths remain supported for publishing.

It is intended for small documents, calculators, visualizations, and tools that do not need a build step. Use the Artifactd design guidance to create a contextual visual language rather than relying on a universal component theme.

## React authoring preset

For stateful or component-heavy artifacts, the optional React/Vite/Mantine authoring preset can be used:

```bash
source_dir="$(artifact create --id my-tool --template react)"
cd "$source_dir"
npm ci
npm run build
artifact publish my-tool
```

The preset is an authoring convenience. It declares its source stack in `artifact.json`, but it builds to the same `web-static` runtime as the plain web template. Artifactd does not install dependencies, run builds, or execute application code; it only serves the generated `dist/` directory.

Mantine provides interaction mechanics and accessible controls. It is not the required Artifactd design language; artifact-specific composition, typography, color, and visual treatment remain the author's responsibility.

## Optional graph support

Add Cytoscape only when the artifact needs a relational graph:

```bash
artifact create --id my-graph --template react --feature graph
```

The graph feature includes a small `public/graph.json` example with `nodes` and `edges`. Replace it with data appropriate to the artifact. The graph renderer is intentionally separate from the application shell so the data model and visual treatment can evolve independently.

## Other stacks

Other frameworks and UI kits may be supported as authoring presets when needed. A future shadcn preset, for example, would describe React, Tailwind, and source-owned shadcn components while still producing `web-static` output.

New presets should provide scaffolding and metadata. They must not require Artifactd to install packages or execute arbitrary build commands. Any build happens in the author's environment before publishing.

## Design guidance

Load the Artifactd design guidance when creating or redesigning an artifact. It applies to static HTML, React, and other authoring stacks alike. It focuses on the artifact's job, visual hierarchy, contextual visuals, responsive composition, accessibility, and complete loading, empty, error, and large-data states.

## Authoring guidelines

- Keep the published output self-contained and free of CDN dependencies.
- Watch the directory that contains the validated `artifact.json` and built entrypoint.
- Use the simplest stack that can produce a reliable result.
- Match the visual representation to the meaning of the data.
- Include loading, empty, error, and large-data states.
- Keep authoring dependencies in the source project, not in the daemon.
- Avoid exposing sensitive workspace data in generated snapshots.
