# Vision

## The problem

CLI agents make custom software cheap to create, but they do not provide a standard place for that software to live. A generated dashboard, calculator, explorer, or visualizer is often left as a file in a workspace. The user has to locate it, figure out how to run it, remember a port, and repeat that process later.

`artifactd` proposes a durable local home for these small applications.

## The product idea

> AI makes software cheap enough that applications can be disposable.

A tool does not need to justify a long implementation project to be worthwhile. Someone might use a mortgage comparison calculator three times, a benchmark viewer every week, and a project dashboard for years. The runtime should support all three without requiring the user to think about deployment.

## Intended experience

A user asks an agent for an interactive tool:

```text
Make an interactive visualization comparing these three architecture approaches.
```

The agent generates a standalone artifact and publishes it:

```bash
artifact create --id architecture-viewer
artifact publish architecture-viewer
```

The user receives a stable URL and can later find the artifact in the local library. Asking the agent to update it should publish a new version of the same artifact, not leave behind an unrelated file.

## What makes it different

- **The artifact is the unit of software.** It is smaller and more disposable than a conventional project.
- **The runtime is agent-neutral.** Any tool that can produce files and run a command can publish an artifact.
- **Persistence is built in.** A successful one-off experiment can become a durable personal application.
- **Local hosting is the default.** There is no deployment ceremony for software that only needs to run on one machine.
- **Safety is part of the foundation.** Generated code should begin with a constrained execution model.

## Examples

- mortgage and household budget calculators
- JSON explorers and CSV visualizers
- architecture diagrams and project status dashboards
- API response inspectors
- GPU sizing and model memory calculators
- benchmark and test-result viewers
- log explorers and repository dashboards

## Long-term direction

The initial static-artifact runtime may grow to support state, capability-mediated APIs, richer runtimes, provenance, agent callbacks, composition, desktop integration, and optional sharing. Those features should be earned by validating the basic publish–reopen loop first.

## Success criterion

The first meaningful test is not technical scale. It is whether a user thinks:

> I asked my agent for a little tool, and now it permanently exists in my artifact library.

If that interaction feels natural and useful, the rest of the platform has a strong foundation.
