# Two repositories: the integration apart from the service and the app – Spec

Status 2026-11-24: decided (the maintainer); built.

## Goal

Home Assistant's Marketplace (since 2026.11) takes one item of one type per repository and refuses a repository with
an app: "A repository holds one item of one type." – "The Marketplace also refuses repositories with apps for Home
Assistant OS." (contributors.home-assistant.io, Marketplace, Repository structure). Mira's repository held the
integration and the HA app together, which HACS allowed. Now the integration stays in miraassist/mira, which people
know and where the Marketplace finds it; the Mira service, the HA app and everything else moved to
miraassist/mira-app, with the history.

## Decisions (2026-11-24, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Who moves | The app and the service (1A); the integration stays at miraassist/mira (its catalog entry, its releases and the review for the default catalog stay with it). An app installed from miraassist/mira is another app than one from miraassist/mira-app to the Supervisor (its id comes from the repository's address): the user adds the new repository, installs Mira from it and takes `mira.yaml` along |
| 2 | The history | The whole history in both (2A): mira-app is a clone of miraassist/mira's main; each side removes the other's files in one commit; nothing is rewritten |
| 3 | The docs | The integration's specs stay with it (3B; `ha-integration.md`); everything else is here, also the guide and the system documentation |
| 4 | Versions | Still one version for both (4A); each repository releases its side with the same tag, this one first |
| 5 | Checking the interface | Locally (5B): with a checkout of the integration beside this one (or `MIRA_INTEGRATION`), `scripts/check.sh` checks that both sides have the same version, and the test Home Assistant mounts the integration from there. CI checks each side alone |
| 6 | README | miraassist/mira's README keeps its showcase at the top: that is the published address. What it said about installing the app and the container is here now; there, links lead here |

## What lives where

- **miraassist/mira:** `custom_components/mira`, `hacs.json`, the integration's pytest tests (`tests/ha`), its
  checks (ruff, pytest, hassfest, the HACS validation) and release (the zip), `docs/spec/ha-integration.md`,
  `docs/brand` (the README's logo), the README with the showcase.
- **miraassist/mira-app:** the service (`cmd`, `internal`, Go module `github.com/miraassist/mira-app`), `app/` and
  `repository.yaml`, `skills`, `evals`, `testenv` and the tests of its fake devices, the page's tests, `docs` (but the
  integration's spec), the release of the binaries, the image (still `ghcr.io/miraassist/mira`) and the app's version.

**Fails:** a checkout of the integration missing → `scripts/check.sh` says so and the version test is skipped; the
test Home Assistant cannot mount the integration → Docker refuses to start it, naming the path.
