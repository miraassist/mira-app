[![Mira](https://raw.githubusercontent.com/miraassist/mira-app/main/docs/brand/mira-logo-small.png)](https://mira-assist.com)

# Mira: the service and the Home Assistant app

**Website: [mira-assist.com](https://mira-assist.com)** · **What Mira can do, and the integration:
[miraassist/mira](https://github.com/miraassist/mira)**

Mira is an always-on voice assistant for Home Assistant. She has two parts in two repositories:

| Part | Repository | What it does |
| --- | --- | --- |
| Integration (Marketplace) | [miraassist/mira](https://github.com/miraassist/mira) | Thin Assist conversation agent that forwards each turn to the Mira service |
| Mira service (Go) and HA app | this one: `cmd/mira`, `internal/`, `app/` | Prompting, tools, memory, messages, security policy; the app runs the service in Home Assistant OS |

Each has its own version; if one is too old for the other, Home Assistant's Repairs say which to update.

## Install the Mira service

First the integration: see [miraassist/mira](https://github.com/miraassist/mira#get-started). Then the Mira
service, one of:

- **Home Assistant OS:** [![Add the app repository to my Home Assistant](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmiraassist%2Fmira-app)
  or Settings → Apps → App store → Repositories → `https://github.com/miraassist/mira-app`, then install and start
  **Mira**. In the integration's setup: Add integration → Mira, address
  `http://localhost:8787`.
- **Docker** on a machine in your home network: a `compose.yaml` in an empty folder, then `docker compose up -d`.
  Home Assistant then finds Mira under **Discovered**.
  ```yaml
  services:
    mira:
      image: ghcr.io/miraassist/mira
      container_name: mira
      restart: unless-stopped
      network_mode: host
      environment:
        TZ: Europe/Berlin
      volumes:
        - ./config:/etc/mira     # mira.yaml, your own skills, what skills bring along
        - ./data:/var/lib/mira   # memory, messages, database
  ```

### Moving the app

Until version 0.30, the HA app came from `https://github.com/miraassist/mira`. Home Assistant's Marketplace takes no
repository with an app any more, so the HA app is here now, and to Home Assistant it is another app, a new Mira. To
move:

1. Copy `mira.yaml` from the old app's config folder (`/addon_configs/…_mira/`). The keys of your AI models are not
   in it, only references such as `${MIRA_MODEL_MAIN_TOKEN}`; the keys stayed in the old app's data. Write them into
   the copy directly, or enter them again on Mira's page under **Models** once she runs.
2. Add `https://github.com/miraassist/mira-app` under Settings → Apps → App store → Repositories (or with the button
   above), install Mira from there, put `mira.yaml` into its config folder, start it.
3. Remove the old Mira entry under Settings → Devices & services and add Mira again (address
   `http://localhost:8787`).
4. Remove the old app and the old repository.

Mira's memory, messages and other data stay with the old app; the new one starts without them.

More: [Installing Mira in detail](docs/install.md) · the guide's setup: [Deutsch](docs/guide/de.md#einrichtung) ·
[English](docs/guide/en.md#setup).

**User guide:** [Deutsch](docs/guide/de.md) · [English](docs/guide/en.md) – also in the web UI under "Anleitung" /
"Guide". **System documentation:** [docs/system.md](docs/system.md) – components, the main flows as diagrams, data,
security.

[![CI](https://github.com/miraassist/mira-app/actions/workflows/ci.yml/badge.svg)](https://github.com/miraassist/mira-app/actions/workflows/ci.yml)

## Development

```sh
go test -race -timeout 120s ./...                     # Mira service
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # the fake devices of testenv
go run ./cmd/testha up                                # test Home Assistant in Docker
scripts/check.sh                                      # all CI checks, before a push
```

`testenv/` holds a throwaway Home Assistant for tests against a real HA; see [testenv/README.md](testenv/README.md).
It mounts the integration from a checkout of [miraassist/mira](https://github.com/miraassist/mira) beside this one
(or `MIRA_INTEGRATION` in `testenv/.env` naming it); `scripts/check.sh` then also checks that neither side's minimum
is above the other's version.

Releases: the service has its own version (`cmd/mira/main.go`); tag `vX.Y.Z` here. It names the oldest integration
it works with (`MinIntegration` in `internal/ha/updates`), raised only when an older integration would fail; such a
release says so first in its notes ([Versions](docs/spec/versions.md)).

## License

Apache License 2.0, see [LICENSE](LICENSE).
