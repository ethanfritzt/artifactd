# Reference artifact

## top-lite

`top-lite` is the maintained production-quality reference artifact. It is a raw HTML/CSS/JavaScript system monitor backed by Artifactd's local system provider and demonstrates how the Artifactd design guidance produces a contextual visual language—a calm laboratory instrument panel—while preserving live CPU, memory, load, process filtering, sorting, and pause behavior.

```bash
artifact publish examples/top-lite
```

The repository intentionally keeps the example set small. Experimental and acceptance-test artifacts should be published to the local Artifactd library rather than committed here.
