# ReelStash documentation

| Read this | If you want to... |
| --- | --- |
| [Quick install](install.md) | Install a full stack (with optional qBittorrent) on a server with one command |
| [Getting started](getting-started.md) | Run the app and index your first media folder |
| [User guide](user-guide.md) | Use libraries, playback, metadata, Admin, and Files |
| [Downloads](downloads.md) | Manage qBittorrent from ReelStash, protect unfinished downloads, move finished ones into libraries |
| [Architecture](architecture.md) | Understand components and data flow |
| [Configuration](configuration.md) | Set environment variables and authentication |
| [HTTP API](api.md) | Integrate with the backend or find route ownership |
| [Plugin API reference](plugin-api.md) | Everything a plugin author needs on one page: bundle format, environment, endpoints |
| [Plugin bundles](plugin-bundles.md) | Install plugins that ReelStash runs for you, or build your own |
| [Provider plugins](provider-plugins.md) | Connect your own metadata source over HTTP |
| [Auth plugins](auth-plugins.md) | Add a way to sign in, such as OpenID Connect |
| [Operations](operations.md) | Deploy, monitor, back up, and troubleshoot |
| [Development](development.md) | Work on the Go and React codebases |

Also see the repository's [testing guide](../TESTING.md) .

The Go router in `backend/internal/httpapi/router.go`, environment loader in `backend/internal/config/config.go`, and Compose files are the authoritative references when implementation changes.
