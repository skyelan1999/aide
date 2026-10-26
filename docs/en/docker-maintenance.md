# Stop Aide and clean Docker build cache

[简体中文](../docker-maintenance.md) · [English documentation](README.md)

Run maintenance launchers from the relevant source checkout or offline installation. Stopping does not delete workspaces, conversations, configuration, data volumes or images. Resume with that installation's original start entry.

| System | Stop this installation | Clean build cache |
| --- | --- | --- |
| macOS | `stop.command` | `clean-docker-cache.command` |
| Linux | `bash stop.sh` | `bash clean-docker-cache.sh` |
| Windows | `stop.bat` | `clean-docker-cache.bat` |
| PowerShell | `./stop.ps1` | `./clean-docker-cache.ps1` |

Shell entries accept `--dry-run`; PowerShell accepts `-DryRun`. Existing command entries are `bash scripts/aide.sh stop` and `bash scripts/aide.sh clean-cache`. If Docker is unavailable, maintenance reports it rather than starting Docker Desktop.

Stop identifies containers from the installation's Compose directory and Aide service labels, supporting custom project names and macOS physical/logical path aliases. It does not guess by port or stop another installation. Repeating stop with no matching running container succeeds. On macOS it also stops that installation's registered host bridges. `.cache/aide-manual-stop` prevents the port watcher recreating the container; ordinary start clears it. Avoid stopping during an A/B update switch.

Cache cleanup runs `docker builder prune --all --force` for the current engine's default builder. This can remove unused build cache shared with other projects, causing later recompilation/downloads. It does not run system/image/container/volume pruning and does not remove images, data volumes, `.env`, workspaces, browser records or offline bundles. Other buildx builders are outside this action. Docker Desktop's disk image may not shrink immediately.

New packages include the appropriate platform maintenance entries; existing packages do not change when source is pushed. Historical tests and Windows-native acceptance remain separate from this documentation update.
