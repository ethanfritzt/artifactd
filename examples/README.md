# Reference artifact

## top-lite

`top-lite` is the maintained production-quality reference artifact. It is a React/Mantine system monitor backed by artifactd's local system provider and demonstrates how the Artifactd design skill produces a contextual visual language—a calm laboratory instrument panel—while preserving live CPU, memory, load, process filtering, sorting, and pause behavior.

```bash
cd examples/top-lite
npm ci
npm run build
artifact publish dist
```

The repository intentionally keeps the example set small. Experimental and acceptance-test artifacts should be published to the local Artifactd library rather than committed here.
