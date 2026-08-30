# Configuration

`artifact` and `artifactd` use the same optional configuration. Configuration can
be supplied with command-line flags, environment variables, or a YAML file.
The precedence is:

1. command-line flags
2. `ARTIFACTD_*` environment variables
3. configuration file
4. built-in defaults

## Configuration file

Pass an explicit file with `--config`:

```bash
artifactd --config ~/.config/artifactd.yaml
artifact --config ~/.config/artifactd.yaml list
```

Without `--config`, Artifactd looks for `.artifactd.yaml` in the home directory
and the current directory. A missing file is allowed. Example:

```yaml
data-dir: /home/alice/.local/share/artifactd
socket: /run/user/1000/artifactd.sock
default-artifact: /home/alice/src/artifactd/default
host: 127.0.0.1
public-host: artifacts.localhost
port: 7337
log-level: info
```

The `--output` flag is a CLI-only setting and is not part of the daemon
configuration.

## Environment variables and flags

The runtime settings below are available to both binaries. Environment names
use the `ARTIFACTD_` prefix; hyphens in flag names become underscores.

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--data-dir` | `ARTIFACTD_DATA_DIR` | `$XDG_DATA_HOME/artifactd`, or `~/.local/share/artifactd` | Registry, versions, staging, and managed sources |
| `--socket` | `ARTIFACTD_SOCKET` | `$XDG_RUNTIME_DIR/artifactd.sock`, or `<data-dir>/artifactd.sock` | Local control socket |
| `--default-artifact` | `ARTIFACTD_DEFAULT_ARTIFACT` | empty | Source directory used to seed `artifactd-home` |
| `--host` | `ARTIFACTD_HOST` | `127.0.0.1` | Browser HTTP listen host |
| `--public-host` | `ARTIFACTD_PUBLIC_HOST` | `artifacts.localhost` | Host suffix used in published URLs |
| `--port` | `ARTIFACTD_PORT` | `7337` | Browser HTTP listen port |
| `--log-level` | `ARTIFACTD_LOG_LEVEL` | `info` | Daemon log level: `debug`, `info`, `warn`, or `error` |

`XDG_DATA_HOME` and `XDG_RUNTIME_DIR` are read directly as standard Linux
XDG settings. They are not prefixed with `ARTIFACTD_`.

Both binaries accept these global flags, but the daemon uses the listen and
logging settings. The CLI uses `--data-dir`, `--socket`, and `--config` to find
the daemon; its `--output` flag selects `table` or `json` output.

## Managed data layout

With the defaults, Artifactd stores data as follows:

```text
~/.local/share/artifactd/
├── registry.db
├── staging/                 # temporary publish and edit uploads
├── sources/<artifact-id>/   # editable managed source
└── artifacts/<id>/versions/ # immutable published versions
```

The daemon creates the data directory and required subdirectories on startup.
Staging content is disposable and is cleaned up on startup or after a request.
Edit sessions themselves are held in daemon memory and do not survive a daemon
restart.

## URL and socket changes

Changing `--port` or `--host` changes where the browser server listens and
changes the port in generated artifact URLs. If `--public-host` is changed,
the configured host suffix must resolve to the local daemon (for example via
local DNS or `/etc/hosts`). The control socket is independent of the browser
listener and remains local-user-only.
