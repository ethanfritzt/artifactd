# Security model

Generated artifacts are code. Even a local-first runtime should assume that an artifact may be buggy, compromised, or intentionally hostile. Security decisions should be made before adding capabilities, not after.

## MVP threat model

The runtime serves HTML, rendered Markdown, CSS, JavaScript, assets, and narrow local data APIs. It should assume:

- an artifact can execute arbitrary JavaScript in its browser context
- an artifact may contain unsafe or deceptive UI
- a published directory may contain symlinks or unexpected files
- local HTTP endpoints may be reachable by other local processes or browser pages
- an artifact may try to access daemon control APIs, localhost services, or user data

Local installation does not make generated code trusted.

## Current constraints

The runtime:

- keep published artifact files immutable
- provide only typed, read-only system and workspace data providers
- provide no arbitrary shell, process-control, or package-install capability
- keep registry mutation, transactional edit, and agent data-push APIs on the Unix socket
- avoid exposing control-plane APIs to artifact pages
- validate paths and prevent traversal outside an artifact version or workspace root
- define symlink handling explicitly
- keep artifact content separate from daemon and library control surfaces
- use restrictive browser headers and a clear content security policy where practical
- accept browser routes only for the configured library host or a validated artifact subdomain
- reject non-canonical URL paths, traversal segments, symlinks, and directory listings when serving snapshots
- render Markdown with raw HTML disabled, dangerous URL filtering, and a 5 MiB document limit
- keep temporary Markdown previews bounded, in memory, absent from the registry, and unavailable within one hour or after daemon shutdown
- upload only the explicitly selected preview file rather than implicitly publishing adjacent files
- scope Markdown preview saving to one selected regular file through a random in-memory capability, exact same-origin checks, atomic replacement, and content-hash conflict detection
- render browser drafts only on live editable preview origins with the same capability/origin checks, bounded input, and safe renderer; draft rendering never changes saved content
- serve the Markdown editor locally, allowing its generated styles through a per-response CSP nonce rather than unrestricted inline styles or third-party script origins
- document the trust model prominently

A static artifact can still make network requests permitted by the browser, including requests to other local services. Artifactd accepts the legacy path form on the configured library host and local loopback aliases. The current browser isolation model uses artifact-specific origins validated from the configured host suffix, restrictive response headers, and a same-origin CSP. That CSP also prevents rendered Markdown from automatically loading remote images. External links remain user-initiated browser navigation. Any expansion of this model requires a new threat-model review.

## Workspace and provider model

Host access is scoped through trusted workspaces and daemon providers rather than repetitive per-artifact prompts. A workspace maps a stable name to a canonical local root, such as `vault → ~/vault`. A published artifact version records its workspace association only after the daemon verifies that the uploaded package matches the source directory; a client-supplied path alone is not trusted.

Read-only providers expose narrow data: system metrics, process summaries, and workspace file metadata. Pi can push structured data from CLI or MCP tools over the user-only Unix socket; credentials remain inside Pi. Artifact pages cannot invoke MCP, execute arbitrary commands, or access the control plane.

Workspace access is still a security boundary. Imported or untrusted artifacts must not automatically inherit access merely because they are copied into a workspace. Writes, secrets, process control, and command execution require separate mediation.

Transactional editing is also a control-plane operation. The browser may observe
edit status and events, but it cannot begin, progress, commit, or abort a
session. Intermediate source files are never served; only a complete validated
package can become an immutable version.

## Open security questions before expansion

- How should workspace trust be represented for imported artifacts?
- Should each artifact receive a unique origin, or can path-based routing be made safe enough?
- How should artifacts be prevented from calling daemon administration endpoints?
- What headers and CSP are compatible with generated visualizations?
- Can thumbnails be generated safely without granting access to the host?
- Should artifacts be able to load remote resources at all?
- What happens when an artifact embeds third-party content?
- How should imported files be handled and disclosed?

## Guiding rule

Convenience should not silently turn a generated web page into a local automation agent. Every expansion beyond static rendering should have an explicit capability and threat-model review.
