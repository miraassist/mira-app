# Integration and service in different versions

Status 2026-10-26: built (the rule for the interface, the repair, the first places that follow it). 2026-11-24:
"Minimum versions instead of one version" decided (the maintainer), not built; it replaces the repair below and the
one version for both. See also [HA connection](ha-integration.md).

## Why

The service and the integration are released together under one version, but they are not updated together. HACS
updates the integration; nobody updates the service's container but the user. For days or weeks an integration may
talk to an older service, or, the other way round, a new service to an older integration. It happened on 2026-10-26:
the integration 0.9.0 from HACS sent persons with the new field `voice_threshold`; the service 0.7.0 writes persons
into `mira.yaml`, which it reads strictly, and an unknown field there would have stopped the whole sync of persons,
satellites and exposure.

## The rule for the interface

Between integration and service, **a new field is always optional**, in both directions:

- **The receiver ignores what it does not know.** A field a newer partner sends is left out, not refused; what the
  receiver does not know waits for its update.
- **The receiver copes with what is missing.** A field an older partner does not send has a default; a path an
  older service does not have (HTTP 404) means "not there yet", not an error.
- **A field never changes its meaning.** What changes in meaning gets a new name; the old one goes only after a
  release in which neither side needs it.

Every change to what the two exchange is checked against this rule (requests and answers of `/v1/*`, the managed
config, the panel proxy).

`mira.yaml` itself stays strict: a typo there is the user's and is shown. Only what the integration sends is taken
leniently, before it becomes part of the file.

## Built

- **Persons from Home Assistant** (`PUT /v1/ui/ha`): of each person the service keeps the fields `mira.yaml` has a key
  for (from the struct `config.Person`, no second list) and logs the others once ("fields from Home Assistant this
  Mira does not know, left out"). Rooms and the rest of the body were read leniently already (JSON).
- **The provider list** (`GET /v1/setup/providers`, since 0.8.0): a service without it answers 404; the integration
  takes that as "no providers" and offers a model of one's own, instead of stopping the setup with an error.
- **The answers** the integration reads (`/v1/health`, `/v1/status`, …) are read with defaults (`.get`), except
  fields every service has sent from the start (`id`, a problem's `name`).

## Minimum versions instead of one version (decided 2026-11-24, not built)

### Why

Since the integration and the service live in two repositories ([Two repositories](repositories.md)), one version for
both means an integration release for every minor release of the service, though the integration changes far less
often: the user updates it and restarts Home Assistant for nothing (0.40.0). The rule for the interface above already
keeps an older partner working; what is missing is a clear line where it no longer does.

### Decisions (2026-11-24, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Who names a minimum | Both (1B): the service names the oldest integration it works with, the integration the oldest service |
| 2 | Version numbers | Each side its own (2A): the integration is released only when it changes and keeps its number until then (0.40.0 → its next release), the service counts on |
| 3 | The repair "versions differ" | Replaced (3A) by the two minimums; being apart is no longer a problem in itself |

### Taken while writing (2026-11-24)

| # | Question | Decision |
|---|---|---|
| 4 | Where the service's minimum is | A constant in the service (`MinIntegration`), sent as the new optional field `min_integration` in `/v1/status` (read every minute) and `/v1/health` (the setup) |
| 5 | Where the integration's minimum is | A constant in the integration (`MIN_SERVICE`, `const.py`); the integration compares it with the service's `version` it already reads |
| 6 | Who shows the repairs | The integration, both: repairs are Home Assistant's |
| 7 | When a minimum is raised | When one side needs something of the other that an older one does not have, and the rule for the interface cannot cover it (an older partner would fail, not only miss a feature). New fields stay optional both ways; a minimum is the exception, named in the release notes |
| 8 | The start values | Both minimums 0.40.0: the versions released together last |
| 9 | Comparing | `x.y.z` numbers, all three parts; a version that is not one (`dev`, a build without a version) is never below a minimum |

### What it does, step by step

1. **The service** answers `/v1/status` and `/v1/health` with `min_integration` (`MinIntegration`). An older service
   sends none: then the integration shows no "update the integration" repair.
2. **The integration**, at every poll of `/v1/status` (and at the setup with `/v1/health`):
   - its own version below `min_integration` → the repair `integration_too_old`: "Mira needs at least version X of
     the Mira integration. Update it under Settings → Marketplace (before Home Assistant 2026.11: in HACS), then
     restart Home Assistant.";
   - the service's `version` below `MIN_SERVICE` → the repair `service_too_old`, worded as today's "service older"
     repair: on Home Assistant OS "Update the Mira app to at least X under Settings → Updates", else "Update the
     Mira service: set its container's image to at least X and start it again" (VE-06's rule);
   - both fine → neither repair; one that was there goes away by itself at the next poll.
   - The old repair `version_mismatch` is deleted at the integration's start, if Home Assistant still has it.
3. **Asking for the update** (the section "Both updates offered together", steps 2 and 3): the service asks the
   Marketplace to look again when the integration (its `X-Mira-Integration`) is below `MinIntegration`, no longer
   when it is only older; the integration asks the Supervisor to reload the store when the service is below
   `MIN_SERVICE`. Once per version, as today.
4. **Releasing**: each repository releases on its own with its own tag. The integration's release keeps its check
   that the tag, `manifest.json` and `const.py` agree. A release that raises a minimum says so in its notes, first.
5. **Checked locally** ([Two repositories](repositories.md), decision 5): with both checkouts side by side,
   `scripts/check.sh` checks that the service's `MinIntegration` is not above the integration's version and the
   integration's `MIN_SERVICE` not above the service's; the test that both versions are equal goes.

### Fails

| Step | Failure | Who learns of it |
|---|---|---|
| 2 | `min_integration` or `version` is no `x.y.z` | no repair (decision 9); a debug line of the integration with the value, once per value |
| 2 | the service is not reached | as today: the coordinator's error; no version repair |
| 3 | the Marketplace or the Supervisor refuses | as today: the log lines of "Both updates offered together" |
| 5 | a minimum above the other side's version | `scripts/check.sh` fails and names both versions |

**Cost:** one more field in two answers; no call, no wait.

### What changes for users

The texts "both share one version: update them together" go: in the guide (both languages), both READMEs, the app's
`DOCS.md`, `CLAUDE.md` (Releasing), the release workflows' comments. They say instead: the integration and the
service are updated on their own; Mira says under Repairs when one is too old for the other.

### Steps

1. **The service**: `MinIntegration`, the field in `/v1/status` and `/v1/health`, the Marketplace's refresh below
   the minimum. VE-07, VE-08.
2. **The integration**: `MIN_SERVICE`, the two repairs (de, en), the old one deleted, the store reload below the
   minimum. VE-09 to VE-12.
3. **Releasing and checking**: the check across both checkouts, the version test gone, the texts above. VE-13.

### Scenarios

- **VE-07** The service names its minimum: `/v1/status` and `/v1/health` carry `min_integration`.
- **VE-08** The Marketplace is asked only below the minimum: an integration older than the service but not below
  `MinIntegration` is not asked for; one below it is, once per version.
- **VE-09** An integration below the service's minimum gets the repair `integration_too_old` with the minimum; at or
  above it none; a service without `min_integration` none.
- **VE-10** A service below the integration's minimum gets the repair `service_too_old`, worded for Home Assistant OS
  or a container; at or above it none.
- **VE-11** Being apart is no problem: an integration 0.40.0 with a service 0.45.0 (minimum 0.40.0) has no repair,
  and the old `version_mismatch` is gone.
- **VE-12** A version that is no `x.y.z` (`dev`) makes no repair.
- **VE-13** `scripts/check.sh` fails when a minimum is above the other checkout's version.

## The repair: which one to update (replaced by the minimums above, once built)

The integration compares its own version with the service's, by major and minor (0.7 against 0.9); a patch apart is
fine.

- **Where the service's version comes from:** `/v1/status`, which the integration reads every minute, says it
  (`version`, new); an older service does not, then the version from `/v1/health` at setup applies.
- **Apart:** a repair in Home Assistant (`version_mismatch`), in simple words, with the next step:
  - the service older, on Home Assistant OS (the HA app): "Update the Mira app to 0.9.0 under Settings → Updates. If
    the update is not offered yet, it appears within a few minutes" (the integration of a release is out minutes
    before the app's image);
  - the service older, elsewhere (a container of one's own): "Update the Mira service: set its container's image to
    0.9.0 and start it again". Which of the two follows from whether the integration runs on Home Assistant OS
    (decided 2026-11-18, VE-06);
  - the service newer: "Update the integration in HACS to 0.10.0 and restart Home Assistant".
- **The same again** (after an update): the repair goes away by itself on the next poll.
- No version at all (a service that does not say one): no repair.

The repair does not prevent anything; the rule above keeps the two working while they are apart, the repair says
how to end it.

## Both updates offered together (decided 2026-11-10, built 2026-11-10, not released)

**Goal:** the user sees the update of the integration and of the HA app at the same time, installs both and restarts
Home Assistant once. Seen 2026-11-10: a user updated the app, the repair said the integration was older, and its
update was offered only after a restart of Home Assistant; then a second restart.

**Why it happens** (Home Assistant 2026.11, `homeassistant/components/marketplace/base.py`): the marketplace checks
installed custom repositories every 48 hours and at every start of Home Assistant (with a GitHub account connected;
without one not at all on a schedule). The Supervisor reloads the app store on its own schedule. The release itself
adds a gap: the GitHub release (the integration) is out minutes before the app's version follows the image
([System](../system.md), Release and updates).

**What changes:**

1. **The release comes out last.** The release job makes the GitHub release as a draft; the app job, after the image
   and the app's version, publishes it. Integration and app become visible in the same minute. **Since 2026-11-24 the
   integration is in its own repository** ([Two repositories](repositories.md)): the service's and app's release
   comes first, the integration's (the same tag in miraassist/mira) right after; step 2 makes its update show at
   once. (A draft is not seen
   by the marketplace: assumption, checked at the first release with it.) If the image or the app job fails, the
   release stays a draft: nothing is offered, the workflow's run shows why.
2. **Mira asks the marketplace to look again.** When the integration is older than the service, the Mira service
   calls `marketplace/repository/refresh` for its repository (the id from `marketplace/repositories/list`, the entry
   with `full_name` `miraassist/mira`), once per version of the service, with the owner's token (the command is for
   admins). Before Home Assistant 2026.11, HACS has the same: `hacs/repositories/list` and `hacs/repository/refresh`
   (admin, `update_repository(force=True)`, then its update entity; HACS's source, `websocket/repository.py`; its
   schedule is the same 48 hours): Mira asks the marketplace first and HACS when Home Assistant does not know the
   marketplace's command. The update of the integration then shows within seconds, without a restart. A patch apart
   counts too (decided 2026-11-13): the repair leaves it alone, but the update is offered for it as well, and asking
   disturbs nobody; until then the rule was the repair's (major or minor), and after 0.22.0 → 0.22.1 nothing was
   asked. Cost: two websocket calls, once per update and start of the service; the list is the whole catalog of
   integrations (some 5,000 entries on the test HA), read once. Fails (neither command, not an admin, GitHub's rate
   limit): a log line `could not ask for the integration's update` with the error; the repair stays as it is and the
   update comes at the next check.
3. **The other way round** (the integration newer than the service: the app's update not offered yet): the
   integration asks the Supervisor to reload the store (`store.reload()` of Home Assistant's Supervisor client,
   `POST /store/reload`), once per start of the integration, only on Home Assistant OS. A patch apart counts here
   too (decided 2026-11-13), as in step 2. Fails: a log line `Could not ask the Supervisor for the app's update`
   with the error; the update comes at the Supervisor's next check (it reloads the store every 3 hours on its own; a
   restart of Home Assistant does not). Not tried, taken from the sources (row 3).

Built: the release workflow (draft, published by the app job), `internal/ha/updates` (the service, on every request
with `X-Mira-Integration`), the integration's header with every request and the store reload in its coordinator.
Tests: `internal/ha/updates` (the marketplace, HACS, neither, the same version, a patch behind),
`tests/ha/test_versions.py` (the header, the reload once, a patch behind without a repair, none for the same or a
newer service, no reload without Home Assistant OS).

**How the service learns the integration's version:** the integration sends `X-Mira-Integration` with every request
to the service (today only through the panel proxy); an older integration sends none, then nothing is asked.

| # | Transition | Trigger | Signal | Checked on the test HA |
|---|---|---|---|---|
| 1 | The marketplace looks at the repository again | `marketplace/repository/refresh` with the repository's id, admin token | the answer `success` (2.4 s); the update entity follows the repository's new version | 2026-11-10: the command answers; at the release of 0.21.0 the test HA's update entity went from v0.20.0 to v0.21.0 2.8 s after it, without a restart |
| 1b | HACS looks at the repository again (before 2026.11) | `hacs/repository/refresh` | as row 1 | not tried: the test HA runs 2026.11 (from HACS's source) |
| 2 | A draft release is not offered | the release job with `draft: true` | the update entity keeps the old latest version | 0.21.0 went out as a draft and was published by the app job (19:02); the test HA still said v0.20.0 after it, as the marketplace had not looked in between: not seen directly |
| 3 | The Supervisor reloads the store | `POST /store/reload` | the Supervisor's event `store_reloaded`; the app's update entity shows the new version | not tried (the test HA has no Supervisor); from the sources, 2026-11-13: the Supervisor fetches every app repository (`git fetch`, depth 1), reads the apps anew when one changed and sends Home Assistant `store_reloaded` (`supervisor/store/__init__.py`, `reload`; since supervisor#7057, July 2026); the hassio integration refreshes its app data on that event at once (`coordinator.py`, `async_refresh_after_store_reload`; since core#177776, August 2026), before that at its next poll (15 minutes). Without the call, the Supervisor reloads every 3 hours (`misc/tasks.py`, `RUN_RELOAD_APPS`) |

## Scenarios

- **VE-01** A newer integration sends a person field the service does not know: the sync goes through, the field is
  left out (test `TestHomeAssistantFieldsFromANewerIntegration`).
- **VE-02** A service a minor version behind: repair "update the service"; a patch apart or the same version: none
  (`test_older_agent_is_a_repair_until_updated`); the service newer: "update the integration".
- **VE-03** A service without `version` in `/v1/status`: the version from `/v1/health` applies.
- **VE-04** A service without the provider list: no providers, no error.
- **VE-05** The start page's footer shows the service's version and, inside Home Assistant, the integration's: its
  proxy sends `X-Mira-Integration` with everything it passes on, the page too; an older integration sends none,
  then only the service's shows (`TestPageShowsTheIntegrationsVersion`, `test_proxy_says_the_integrations_version`).

## Later

- **One release channel:** a Home Assistant app updates the service with the integration
  ([App](app.md), built, not yet tried on Home Assistant OS).
- **An update entity** ("Mira service 0.10.0 available") instead of a repair, where Home Assistant cannot update
  the service itself: open.
