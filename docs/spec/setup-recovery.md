# A setup that does not finish – spec

Status 2026-11-26: decided (the maintainer), nothing built. Scenarios: [setup-recovery.feature](setup-recovery.feature)
(SU-01 to SU-09). Found by issue 10 of miraassist/mira (moving to the app of 0.40.0) and reproduced on the test HA.
Hangs on [Setup](setup.md) (the pairing, the kept token) and on [Reading what comes in](parsing.md) (the limited
start, row 7).

## Goal

Two faults kept a user out of Mira:

1. **A limited start cannot be set up.** The limited start serves the setup mode's server, so its `/v1/health` says
   `status: setup` even after the pairing. The integration's setup waits for another status (`_wait_ready`), gives up
   after a minute ("the service did not start within a minute") and makes no config entry. A broken `mira.yaml`
   therefore never reaches the repair "Mira's settings cannot be read" it was built for (parsing.md, row 7), unless
   an entry existed before. Mira's page meanwhile says "Mira is waiting to be set up".
2. **A setup that fails after the pairing leaves Mira paired without an entry.** The pairing is done on the
   service's side once `POST /v1/pair` answers; whatever fails afterwards (the wait, a restart of Home Assistant, the
   dialog closed) leaves no entry. A new dialog then asks for a pairing code, which the service writes into its log
   only when an entry is removed: there is none. The only way out is a reset.

Reproduced 2026-11-26 with the service and the integration of 0.40.0: a `mira.yaml` with three models whose tokens
referred to the secrets file of another installation; both clicks on "Start using Mira" ended after 60 s without an
entry, `GET /v1/pair` said `open: false, pairs: true`, a new dialog asked for a code, and only "Reset data" (after the
file was corrected) led to an entry.

## Decisions (2026-11-26, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | What the setup does with a limited start | It finishes: the limited start says `status: limited` in its health once Home Assistant's access is set, the setup takes that as started and makes the entry; the repair "Mira's settings cannot be read" names the file, the line and the error (1A). Not: the dialog shows the error and makes no entry (that leaves fault 2) |
| 2 | The way out of a pairing without an entry | The integration keeps the token as soon as the pairing answers, as a kept token (`mira.rejoin`); a new dialog finds it and needs no code (2A). Not: a pairing in two phases on the service's side (more code on both sides); not: an entry made anyway on a failure (does not help when the dialog is closed) |
| 3 | Mira's page in the limited start | It says that Mira's settings cannot be read and where to see why, instead of "waiting to be set up" (3A) |

## What it does

### The limited start's health (decision 1)

- `/v1/health` of the limited start says `status: limited` once Home Assistant's access is set (from the setup in the
  database: the address and the token); while it is missing, `status: setup` as now. So a limited start that still
  waits for its setup is set up as any service in setup mode (the summary, the model, "Start using Mira"); only the
  wait after "Start using Mira" ends at once.
- The health carries no error text: it is answered without a token in the setup mode, and a YAML error can quote a
  line of the file, a secret too. The problem with the file, the line and the error stays in `/v1/status`, which needs
  the token, as now (parsing.md, row 7).
- The setup's wait (`_wait_ready`) already takes every status other than `setup` as started; `limited` ends it with
  the first answer. The entry is made as after any setup. Its first status brings the problem, and the integration
  makes the repair `config_problem` (built 2026-11-23). Voice turns get the sentence that Mira's settings cannot be
  read (PA-07). Once the file is corrected, Mira starts anew (PA-08) and the repair goes.
- **The interface** ([Versions](versions.md), new fields optional both ways): `limited` is a new value of an existing
  field. An integration before this change compares only with `setup` and finishes as well, so it needs no update for
  fault 1. A service before this change says `setup` in its limited start: with a new integration the wait still ends
  after a minute, but the token is kept (decision 2), and once the file is corrected, adding Mira again works without a
  code. Neither side raises the other's minimum.
- **Fails:** the store cannot be read at the start → the limited start ends as now ("the limited start failed too"),
  the app's log says why. `/v1/status` refuses the entry's token after the setup → the integration's existing repair
  for a refused token. The first status cannot be fetched → the coordinator's existing error and retry; the repair
  comes with the first status that arrives.

### The kept token (decision 2)

- As soon as `POST /v1/pair` answers with the token, before the wait, the setup keeps it under the service's id
  (`keep_rejoin`, the same storage `mira.rejoin` an entry's removal uses). One write to Home Assistant's storage per
  pairing; nothing else waits on it.
- A finished setup forgets it, as now (`forget_rejoin` before the entry is made); the entry holds the token.
- A new dialog for this service finds the kept token as it finds one of a removed entry whose service was away
  (setup.md, "Removing releases"): `GET /v1/pair` says `open: false`, the kept token works, the summary shows no code,
  and "Start using Mira" sends the setup with it (`POST /v1/setup`). In the setup mode `/v1/health` is answered without
  checking the token, so a kept token that no longer holds shows only at `POST /v1/setup`: the service refuses it
  (401), and the summary says the pairing did not work, as now.
- The token never leaves Home Assistant other than to this service, as the entry's token does; the storage is Home
  Assistant's private storage, as for a removed entry.
- **The dialog's sentence after a failed wait** says what is true now: Mira is paired but did not start; the app's log
  or the service's log says why; adding Mira again afterwards needs no code. The sentence for a failed pairing ("Pairing
  did not work") stays for an error of `POST /v1/pair` itself.
- **Fails:** keeping the token fails (Home Assistant's storage refuses the write) → a warning in Home Assistant's log
  with the service's id (never the token), and the setup goes on; only a later failure would then need the reset, as
  before. A service without an id (before ids existed) does not pair, so it has no kept token.

### Mira's page in the limited start (decision 3)

- Heading "Mira kann ihre Einstellungen nicht lesen" (with the line in English below, as the page has now): the file
  `mira.yaml` cannot be read; Repairs in Home Assistant and the Mira app's log name the line and the error; once the
  file reads, Mira starts anew by herself.
- While the limited start still waits for its setup, the page also shows the steps of the setup as now; with a pending
  pairing code, the hint where to find it, as now.
- No error text, for the reason of decision 1: the page is open without a token.
- **Fails:** nothing new; the page is built from what the limited start has.

## Costs

- The setup's wait after "Start using Mira": one `GET /v1/health` per second as now (60 at most); on a limited start
  it ends with the first answer after the restart instead of after a minute.
- One write to Home Assistant's storage per pairing (the kept token), one more when the setup finishes (forgotten, as
  now).
- Nothing grows with the number of anything.

## Steps

1. **The service** (decisions 1 and 3): the limited start's health says `limited` once Home Assistant's access is set;
   its page says that the settings cannot be read, without the error. SU-01 to SU-04 in Go.
2. **The integration** (decision 2): the token kept as soon as the pairing answers; the dialog's sentence after a failed
   wait (de, en); the wait taking `limited` as started. SU-05 to SU-09 in pytest.
3. **End to end on the test HA**, as reproduced on 2026-11-26 (a second service with a `mira.yaml` whose tokens are
   missing): the setup finishes with an entry and the repair; correcting the file starts Mira and removes the repair;
   with a service that stays in setup mode (an old one) the wait ends after a minute and a new dialog needs no code; the
   integration of 0.40.0 with the new service finishes too. The release notes say what a user stuck like in issue 10
   does.
