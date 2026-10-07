# Agent integration

Agent support should begin with ordinary shell commands. No SDK or custom protocol should be required to publish a useful artifact.

## Shell-first workflow

A generic agent instruction can be:

```text
When an interactive visual, calculator, dashboard, explorer, or reusable UI
would be more useful than plain text:

1. Create an Artifactd-managed source with `artifact create --id <id>`; the initial scaffold is published automatically.
2. Begin an editing session with `artifact edit begin <id> --message "..."` before generating files.
3. Generate the artifact files in the returned source directory; optionally send progress messages with `artifact edit progress`.
4. Commit the complete artifact with `artifact edit commit <id> --session <session-id>` and return the artifact URL. Use `artifact create --open` when the user wants the initial URL opened automatically.

Use `artifact create --path <directory> --id <id>` only when repository-local
source is intentional.
```

For a request that only needs to work with an existing Markdown file, use `artifact preview <file> --open` instead of creating a durable artifact. The temporary preview contains only that file, stays out of the library, expires automatically, and offers explicit editing/saving plus PDF and DOCX export.

This keeps `artifactd` agent-agnostic. Pi, Codex, Claude Code, OpenCode, shell scripts, and humans can all use the same interface. A source directory under a registered workspace is associated automatically when published if the daemon verifies that its complete file set and contents match the uploaded package; otherwise the publish remains unassociated.

## Expected agent behavior

An integration should encourage agents to:

- prefer a self-contained artifact for the MVP
- choose a stable, descriptive identifier when updating an existing artifact
- avoid embedding secrets or private credentials in published files
- publish only the intended directory
- report the resulting URL clearly
- use `artifact edit commit` for coordinated updates and `artifact publish` for direct checkpoints
- update the existing artifact ID instead of creating duplicates

## Agent guidance

This repository documents the agent workflow but does not ship an installable
Agent Skill package. Integrations should use the public CLI and the workflow
above rather than relying on internal daemon APIs. The authoring and security
documents provide the design, accessibility, and reliability guidance needed
for generated artifacts.

## Pi integration

A Pi integration should use this documented workflow or an equivalent instruction file. It should not need to call internal daemon APIs or depend on Pi-specific runtime behavior.

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

## Transactional artifact editing

After `artifact create` has published the initial scaffold, begin a session before making a multi-file change:

```bash
artifact edit begin demo --message "Redesigning the dashboard" --output json
# keep the returned session_id and edit the returned managed source directory
artifact edit progress demo --session <session-id> --message "Finishing responsive styles"
artifact edit commit demo --session <session-id>
```

The begin command tells Artifactd and the browser that editing has started. Pi can then write the managed source files normally, but must keep the manifest ID unchanged and send progress for edits lasting longer than five minutes. Artifactd does not watch intermediate filesystem changes and never serves an incomplete source tree. Commit validates and publishes exactly one immutable version, then the browser reloads the stable URL. If validation fails, the previous version remains served and the session can be retried. Abort an abandoned session with:

```bash
artifact edit abort demo --session <session-id>
```

Sessions expire after inactivity. The protocol is agent-agnostic: Pi, another shell-capable agent, or a human can use the same commands.

## Future agent actions

A later artifact feature could send a structured action back to an agent:

```text
artifact action:
Add Redis as a caching layer to this architecture.
```

That would create a loop of artifact → agent → updated artifact. It should require an explicit, authenticated local protocol and must not permit arbitrary command strings.

## Compatibility principle

The artifact protocol should remain useful without any specific agent. Agent integrations are clients and helpers, not owners of the registry or artifact lifecycle.
