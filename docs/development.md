# Development

Artifactd targets Linux and requires Go `1.25.5` or newer. The daemon runs in
the foreground; use a second terminal for CLI commands.

## Build and test

Build both native binaries:

```bash
make build
```

Validate the repository's raw static artifacts (there is no Node build step):

```bash
make web-build
```

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

This installs `artifact` and `artifactd` in `~/.local/bin` and copies the
repository's default artifact to `~/.local/share/artifactd/default`. Ensure
`~/.local/bin` is on `PATH`, or invoke the binaries by their full path.

Artifacts in this repository are standalone HTML, CSS, and JavaScript files. The daemon serves them directly and never runs Node, installs packages, or executes application builds.

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
