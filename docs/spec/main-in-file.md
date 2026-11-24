# The main model in mira.yaml – Spec

Status 2026-11-24: steps 1 and 2 built (not released): the setup writes `main` into mira.yaml, the page edits it; an installation's model of before moves into the file at its first start, and when the file cannot take it, Mira runs as before and says so (repair `model_file`, the integration's text in its next release). Scenarios: [main-in-file.feature](main-in-file.feature).

## Goal

Today the model the integration's setup chooses (`main`) is kept in Mira's database (`kv` key `setup.llm`) and laid
over `mira.yaml` at every start (`setup.Apply`). So that this does not silently undo a change, Mira's page shows
`main` read only with a grey hint ("change it in Home Assistant under Configure",
[Editing](editing.md), decision 6). Users look for the models on Mira's page, find `main` locked and its save button
grey, and think saving is broken (reported 2026-11-24 by a user of the Home Assistant app).

After this spec `main` is an entry in `mira.yaml` like every other model, its key in the secrets file like every
other key the page writes. The integration's setup and its "Configure → AI model" write that entry through the
service; Mira's page edits it. There is one place for the model, the file, and two forms that change it.

## The concept it hangs on

- **`mira.yaml` is the one truth, the page writes it** ([Editing](editing.md), decision 1;
  one place for a setting, CLAUDE.md). The setup's model was the named exception
  ([Cleaning up kv](kv-cleanup.md), decision 2); this spec ends that exception for the model.
- **The page's write path** (`config.File.Edit` with the secrets file, [Editing](editing.md), decision 2): the setup
  writes `main` the same way the page's model form does, with the same secret name
  (`MIRA_MODEL_MAIN_TOKEN`).
- **Departure:** Home Assistant's address and token and the pairing stay in `kv` as before
  ([Cleaning up kv](kv-cleanup.md), decision 2 holds for them): the integration renews the token, the user never
  edits it, and the page has no form for it.

## Decisions (2026-11-24)

| # | Question | Decision |
|---|---|---|
| 1 | Where `main` lives | In `mira.yaml` under `models.main`, its key in the secrets file. `setup.llm` in `kv` is no longer written. Replaces [Cleaning up kv](kv-cleanup.md), decision 2, for the model, and [Editing](editing.md), decision 6 |
| 2 | The integration's "Configure → AI model" | Stays. It sends the model as today (`POST /v1/setup` with `llm`); the service writes it into `mira.yaml`. The page and the integration change the same entry; the later save wins, as between two tabs of the page |
| 3 | Existing installations | At the first start of the new version Mira moves `setup.llm` into `mira.yaml` and the secrets file, then deletes the key. When the file cannot be written, Mira runs with the value from the database as before, says so in the log and as a repair, and keeps `main` read only on the page with the reason; the next start tries again |
| 4 | Reset (`mira reset`, the app's `reset_data`) | Keeps the model: it is in `mira.yaml`, which a reset keeps. The setup then shows the model as "stays …" and does not ask for one |
| 5 | Interface | Unchanged fields both ways; only what the service does with `llm` changes. An integration of 0.40 works with the new service and the other way round. `MIN_SERVICE` and `MinIntegration` stay |
| 6 | A setup into a read-only `mira.yaml` (a container whose file is mounted read only) | The setup fails and says what to do: the integration's form shows the service's error with the file and the cause; nothing goes into the database. Before, it worked, because the model went into the database; the file stays the one truth. Replaces [Cleaning up kv](kv-cleanup.md), decision 8, for the model |

## What the setup writes

On `POST /v1/pair` and `POST /v1/setup` with `llm` (after the trial request, as today), the service writes
`models.main` in `mira.yaml`. What the setup decides is replaced; what the setup knows nothing of stays:

| Field | Written | Was in `setup.Apply` |
|---|---|---|
| `endpoint` | `base_url`, without a trailing `/` | same |
| `model` | `model` | same |
| `token` | `${MIRA_MODEL_MAIN_TOKEN}`, the key in the secrets file; without a key: the entry's `token` and the secret removed | same (the key itself) |
| `privacy` | `cloud` for a provider, `local` for an own server | same |
| `context_tokens` | what the server said; removed when it said nothing (the old size belonged to another model) | kept the file's |
| `cache_prompt`, `warmup_on_start`, `warmup_on_prefix_change` | `true` for a local model, removed for a cloud model | same |
| `warm_slots` | removed for a cloud model, kept for a local one | not touched |
| `parallel`, `pin_slots` | removed: the default by privacy applies | set to 4 or 1, slots cleared |
| everything else (`fallback`, `timeout`, `temperature`, `reasoning_effort`, `max_tool_rounds`, …) | kept | kept |

- `main` is written at its place under `models`; a file without `models` gets the section. A file that is missing is
  created (as the page does, `config.ConfigPath`).
- The secret goes first, then the file; when the file fails, the secret goes back as it was (the page's
  `editor.write`, which moves into `internal/config` so both use it).
- **One writer at a time:** `config.File.Edit` takes a lock per file. The page, the schedules' writes and the setup
  all go through it, so a setup during a save on the page waits for it instead of losing one of the two.
- After writing, the service restarts as today; the new `main` holds from the start.

**Cost:** one read and one write of `mira.yaml` and of the secrets file per setup or per "Configure → AI model",
beside the trial request the model already got. Nothing on a turn.

## The page

- `main` is edited like every model: the lock, the hint "from the setup in Home Assistant" and the error
  "main kommt aus der Einrichtung …" go. `main` still cannot be renamed or deleted.
- Only in the case of decision 3 (moving failed) `main` stays read only, and the hint says why: the file cannot be
  written, with the cause.
- The catalog's "from Home Assistant" beside main's address goes with it.

## Moving an existing installation (decision 3)

At start, before `setup.Apply`, when `kv` holds `setup.llm`:

1. The values are written as under "What the setup writes", over a `main` the file may have: the setup's values
   were the ones in use, so nothing changes for the running Mira. The log names the settings of the file this
   replaced (`models.main.endpoint`, `.model`, `.token`, as `setup.Covered` today), never the key.
2. `setup.llm` is deleted from `kv`.
3. A log line: "the setup's model moved into mira.yaml" with the file.

Each start does this only while `setup.llm` is there, so once it is moved nothing more happens.

## When a step fails, and who learns of it

| Step | Fails | What happens | Who learns of it |
|---|---|---|---|
| Setup writes `main` | `mira.yaml` or its folder is read only; the secrets file is encrypted and `MIRA_SECRET_KEY` is missing or wrong; the file does not parse; the result does not validate | Nothing is written, the secret is put back, the service does not restart | The integration's form shows the service's error (422 with the cause and the file); the service's log names the file and the cause (decision 6) |
| Setup writes `main` | The secret could not be put back after the file failed | As on the page ([Silent fallbacks](fallbacks.md), row 31) | Log line "the change failed and its secrets could not be put back" |
| Moving at start | The file or the secrets file cannot be written, or the result does not validate | Mira runs with `setup.llm` laid over the file as before; `setup.llm` stays; the next start tries again | Log line (warn) with the file and the cause; a problem in `/v1/status` with source `model_file`, name `main`, the cause as text → the integration's repair "Mira kann ihr KI-Modell nicht in die mira.yaml übernehmen" / "Mira can't move her AI model into mira.yaml" with the cause and: make the file writable, then restart the app; the page shows `main` read only with the cause |
| Moving at start | `setup.llm` does not parse | As today in `setup.Load` (the value is left out) | **New:** a log line with the key, the value's length and the parse error, not the value's start: it holds the model's key; Mira then waits for the setup as without a model |
| Moving at start | Deleting `setup.llm` fails after the file was written | The file holds; the next start writes the same values again | Log line with the error |

The source `model_file` is new: an integration before the release that knows it shows it with a skill's text and
does not remove it (as with `model`, [Models](models.md)). That integration's release is a minor version and
says so; `MIN_SERVICE` does not change.

## Steps

1. **The setup writes `main` into `mira.yaml`**: `/v1/pair` and `/v1/setup` write the entry and the secret through
   the shared write path with its lock; `setup.llm` is no longer written nor laid over; the page edits `main`;
   `GET /v1/pair` and `/v1/health` name the model from the file. MF-01 to MF-08.
2. **Moving existing installations**: `setup.llm` into the file at start, the failure path with the source
   `model_file` and the integration's repair for it. MF-10 to MF-15.
3. **Documents**: [Models](models.md) ("What the integration sets"), [Editing](editing.md) (decision 6 replaced),
   [Setup](setup.md) (reset keeps the model), [Cleaning up kv](kv-cleanup.md) (decision 2 for the model),
   [Database](../database.md) (`setup.llm` gone), the guide in both languages and the app's DOCS.md (where the AI
   model is changed: on Mira's page or under Configure).
