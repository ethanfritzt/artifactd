# Agent integration

Agent support should begin with ordinary shell commands. No SDK or custom protocol should be required to publish a useful artifact.

## Shell-first workflow

A generic agent instruction can be:

```text
When an interactive visual, calculator, dashboard, explorer, or reusable UI
would be more useful than plain text:

1. Create an Artifactd-managed source with `artifact create --id <id>`; the initial scaffold is published automatically.
2. Generate the artifact files in the returned source directory.
3. Run `artifact watch <id>` for a live preview while editing, or use `artifact create --open` when the user wants the initial URL opened automatically.
4. Run `artifact publish <id>` for an explicit immutable checkpoint and return the artifact URL.

Use `artifact create --path <directory> --id <id>` only when repository-local
source is intentional.
```

This keeps `artifactd` agent-agnostic. Pi, Codex, Claude Code, OpenCode, shell scripts, and humans can all use the same interface. A source directory under a registered workspace is associated automatically when published.

## Expected agent behavior

An integration should encourage agents to:

- prefer a self-contained artifact for the MVP
- choose a stable, descriptive identifier when updating an existing artifact
- avoid embedding secrets or private credentials in published files
- publish only the intended directory
- report the resulting URL clearly
- publish updates to the existing artifact instead of creating duplicates

## Shipped skill

The repository includes two Agent Skills-compatible instruction packages:

- [`skills/artifactd-cli/SKILL.md`](../skills/artifactd-cli/SKILL.md) for creating, building, and publishing artifacts through the public CLI
- [`skills/artifactd-design/SKILL.md`](../skills/artifactd-design/SKILL.md) for contextual visual direction, responsive composition, accessibility, and reliability review

They use public authoring and runtime interfaces and do not depend on internal daemon APIs.

Once this repository is available as a package, an agent can install the skill with:

```bash
npx skills add <owner>/artifactd --skill artifactd-cli --skill artifactd-design --agent pi --copy
```

For local development, the file can be loaded directly from the `skills/artifactd-cli` directory.

## Pi integration

The Pi integration should use the shipped skill or an equivalent instruction file. It should not need to call internal daemon APIs or depend on Pi-specific runtime behavior.

An integration can open the returned URL automatically by passing `--open` to `artifact publish`. Other future conveniences may include:

- detecting when an artifact is a better response than prose
- associating provenance with the current workspace and prompt
- asking the user before replacing an existing artifact
- requesting an artifact update from an interactive UI

## Agent data sources

The agent can publish a snapshot by writing structured data such as `data.json` into the artifact directory and publishing it. This is appropriate for GitHub, Git, and other CLI/MCP-backed dashboards:

```text
Pi → CLI or MCP → data.json → artifact publish
```

For live data, push JSON to a named runtime source:

```bash
artifact data push project-dashboard github --file data.json
```

The artifact reads that source from `/_artifactd/data/github`. MCP credentials and command execution remain inside the agent. Runtime data is ephemeral and does not mutate immutable artifact versions.

## Live artifact editing

After `artifact create` has published the initial scaffold, Pi can keep the artifact open while editing its source directory:

```bash
artifact watch demo
```

These commands resolve `demo` to the managed source directory. Existing source
paths remain supported for artifacts created before managed sources were added.

`artifact watch` starts an opt-in local live preview and prints the same stable URL. File changes are validated, copied into an atomic temporary snapshot, and cause the browser to refresh automatically. While a snapshot is being assembled, the existing page is blurred and covered by a loading indicator. Invalid or incomplete edits leave the last valid preview available. A later `artifact publish` remains the explicit durable checkpoint and creates an immutable version; ordinary saves do not create versions.

Watch mode is intended for a foreground agent session and should be stopped with `Ctrl-C` or:

```bash
artifact unwatch demo
```

Watch the raw source directory because artifactd serves static files directly and does not run build commands.

## Future agent actions

A later artifact feature could send a structured action back to an agent:

```text
artifact action:
Add Redis as a caching layer to this architecture.
```

That would create a loop of artifact → agent → updated artifact. It should require an explicit, authenticated local protocol and must not permit arbitrary command strings.

## Compatibility principle

The artifact protocol should remain useful without any specific agent. Agent integrations are clients and helpers, not owners of the registry or artifact lifecycle.
