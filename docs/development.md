# Development

Artifactd targets Linux and requires Go `1.25.5` or newer. The daemon runs in
the foreground; use a second terminal for CLI commands.

## Build and test

Build both native binaries:

```bash
make build
```

Validate the repository's raw static artifacts (no Node build is needed for artifacts or normal Go builds):

```bash
make web-build
```

### Markdown editor assets

The daemon-owned Markdown editor uses locally bundled CodeMirror 6. Its
generated JavaScript and third-party license notices in `internal/web/assets/`
are committed and embedded into the Go binary. There are no CDN requests and
the daemon never runs Node. Only editor development needs Node/npm:

```bash
make editor-build
# Install a test browser once:
cd internal/web/editor && npx playwright install chromium && cd ../../..
make editor-test
# Alternatively use a locally installed Chromium-compatible browser:
CHROME_PATH=/usr/bin/google-chrome make editor-test
```

Edit `internal/web/editor/editor.js`, then regenerate the bundle with
`npm run --prefix internal/web/editor build`. Commit both source and generated
assets. The lockfile pins the bundler and editor dependencies.
The optional `browser`-tagged Go test starts isolated preview fixtures and runs
Playwright coverage for draft preservation, formatting, save/conflict handling,
preview races, responsive layouts, dark mode, large files, CSP, and print styles.

### Go validation

Run the test suite and static checks:

```bash
make test
make vet
make lint
```

Install binaries and the raw default artifact into the user data directory:

```bash
make install
```

This installs `artifact` and `artifactd` in `~/.local/bin`, copies the
repository's default artifact to `~/.local/share/artifactd/default`, and
registers Artifactd as an optional Markdown application in the user's Linux
desktop MIME database. Ensure `~/.local/bin` is on `PATH`, or invoke the
binaries by their full path.

Artifacts in this repository are standalone HTML, Markdown, CSS, and JavaScript files. The daemon serves regular assets directly, renders Markdown itself, and never runs Node, installs packages, or executes application builds.

The daemon runs in the foreground during development. When started from the repository root, it discovers the raw default artifact under `default/` automatically:

```bash
./bin/artifactd
```

After `make install`, start it from any directory with:

```bash
artifactd
```

For a smoke test, use a second terminal:

```bash
artifact create --id demo --open
artifact preview ./README.md --open
artifact list
artifact edit begin demo --message "Testing the edit flow" --output json
# edit ~/.local/share/artifactd/sources/demo/ files
artifact edit commit demo --session <session-id>
```

Stop the foreground daemon with `Ctrl-C`. A user service template is provided for optional supervision. Install it once
before enabling it:

```bash
mkdir -p ~/.config/systemd/user
cp contrib/artifactd.service ~/.config/systemd/user/artifactd.service
systemctl --user daemon-reload
systemctl --user enable --now artifactd.service
systemctl --user status artifactd.service
```

The service uses the installed binaries and default data directory. Override
its environment or command with a user-unit drop-in when using custom paths;
see [Configuration](configuration.md).

Builds can embed a release version with linker flags through the `VERSION` Make variable. See [Configuration](configuration.md) for environment variables, custom data directories, and socket paths.
