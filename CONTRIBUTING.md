# Contributing

`artifactd` is in the pre-alpha Phase 1 implementation stage. Contributions that clarify the publish workflow, reduce MVP scope, or expose important security and usability questions are especially welcome.

## Before implementation

Please prefer changes that:

- sharpen the publish–reopen experience
- keep the MVP small and static
- preserve agent neutrality
- make safety assumptions explicit
- turn open questions into testable decisions
- distinguish committed behavior from future ideas

Avoid introducing implementation scaffolding solely for speculative features. The project should validate the core workflow before adding runtimes, SDKs, persistence APIs, or cloud functionality.

## Documentation guidelines

- Use concrete user workflows and examples.
- Mark provisional APIs and unresolved decisions clearly.
- Keep terminology consistent: artifact, manifest, version, library, runtime, and daemon.
- Update relevant documents when changing scope or principles.
- Do not describe planned features as implemented features.

## Proposed workflow for changes

1. Open an issue or discussion for a substantial product or architecture change.
2. Explain the user problem and why it belongs in the current phase.
3. Update the relevant documentation first.
4. Keep implementation changes separate from design exploration when possible.

The implementation can be built and tested with `make build` and `make test`. Keep changes focused on the Phase 1 CLI, daemon, storage, IPC, and static-serving workflow.
