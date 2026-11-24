[![Mira](https://raw.githubusercontent.com/miraassist/mira-app/main/docs/brand/mira-logo-small.png)](https://mira-assist.com)

# Mira: the service and the Home Assistant app

**Website: [mira-assist.com](https://mira-assist.com)** · **What Mira can do, and the integration:
[miraassist/mira](https://github.com/miraassist/mira)**

Mira is an always-on voice assistant for Home Assistant. She has two parts in two repositories:

| Part | Repository | What it does |
| --- | --- | --- |
| Integration (Marketplace) | [miraassist/mira](https://github.com/miraassist/mira) | Thin Assist conversation agent that forwards each turn to the Mira service |
| Mira service (Go) and HA app | this one: `cmd/mira`, `internal/`, `app/` | Prompting, tools, memory, messages, security policy; the app runs the service in Home Assistant OS |

Both share one version: update them together.

## Install the Mira service

First the integration: see [miraassist/mira](https://github.com/miraassist/mira#get-started). Then the Mira
service, one of:

- **Home Assistant OS:** Settings → Apps → App store → Repositories → `https://github.com/miraassist/mira-app`,
  then install and start **Mira**. In the integration's setup: Add integration → Mira, address
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
(or `MIRA_INTEGRATION` in `testenv/.env` naming it); `scripts/check.sh` then also checks that both sides have the
same version.

Releases: the Mira service and the integration share one version. Bump it in `cmd/mira/main.go` here, and in the
integration's `const.py` and `manifest.json`; then tag `vX.Y.Z` here first (the image and the app), then the same tag
in the integration's repository.

## License

Apache License 2.0, see [LICENSE](LICENSE).
