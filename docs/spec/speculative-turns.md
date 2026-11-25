# Speculative turns

Status 2026-11-25: decisions 1 to 7 decided; step 1 built (the subscription; its events are only logged until step 2). Optional: without
`speculation.events_url` in `mira.yaml` nothing changes. Scenarios: [speculative-turns.feature](speculative-turns.feature).

## Goal

At a satellite a turn reaches Mira only after Home Assistant has decided the user is done (the satellite's or Home
Assistant's silence wait) and the speech-to-text has finished. Then the model starts. Measured on 241 real turns of
one house (2026-11-25): from the last word to the text at Mira 0.87 s (p50), 1.96 s (p90); the first round of the model
comes on top.

A **side channel** beside the speech-to-text reports what is being said while the satellite still listens: at each
pause a **reading** of the audio so far, with a guess whether the user is done. Mira starts the model on a reading, a
**speculative run**: it only reads, keeps its answer and speaks nothing. When Home Assistant calls with the turn and the
turn's model request is the same as the run's, the turn **takes over** the run's rounds instead of calling the model.
On 241 real turns the reading 200 ms after the last word was the final text in 89 %.

What it saves is the wait for the first round, mostly; with a cloud model (Gemini Flash Lite) also its network time.
Nothing is said or done earlier than today: the answer still starts when Home Assistant calls, and every action
still runs inside the turn.

## Not part of it

- **Answering before Home Assistant calls.** Home Assistant decides when the user is done; a run is never spoken. A
  turn Home Assistant handles itself (a local intent) or drops leaves the run unused.
- **Acting before Home Assistant calls.** A run runs only tools that only read. Everything else waits for the turn.
- **Changing the end of speech.** The satellite's and Home Assistant's silence waits stay as they are.
- **The integration, Home Assistant.** Neither changes; the side channel needs no field of the conversation API.
- **The side channel itself.** It belongs to the speech services, not to Mira: the reference is the Qwen3-ASR server
  of `ha-voice-rocm` (its `docs/side-channel.md`). Mira reads its events and needs nothing else of it.

## Building blocks

- **The turn as it is** (`conversation.Engine.Handle`, `turn`): the run builds its model request with the same code,
  only without what changes state (below), and the turn decides, round by round, whether a run's round is its own.
- **speaker-id** ([Speaker recognition](speaker-id.md)): the same pattern of an optional service beside the
  speech-to-text that Mira asks by the turn's text and time; off without a URL.
- **Ducking** ([Music](music-players.md), part 7): Mira already watches the satellites' states; `listening` names
  the satellite a side-channel turn belongs to.
- **The model's places** (`agent.Runtime.Acquire`, `parallel`): a run takes a free place or none.
- **Retries** ([Retries](retry.md)): the subscription to the side channel is one more row of the table.

Departure: a run is not an agent run (`agent.Runtime.Call`). It is the first part of a turn computed early, not a
helper with a job of its own; it writes no run row and has no result of its own.

## Decided (2026-11-25)

| # | Question | Decision |
|---|---|---|
| 1 | Which satellite, room and person a run is for | The one satellite in `listening` when the side channel opens its turn gives the device and its room; with none or several in `listening` there is no run. The person is found as a turn finds it, with the voice taken from the last person speaker-id recognized at this satellite (speaker-id knows the turn's voice only after the transcript); the conversation's person in a running conversation. Whether the guess was right decides the request comparison (2) |
| 2 | When a turn takes over | Only when the turn's model request for a round is the same as the run's for that round: the same messages and the same tools, the same model and settings, compared as their hash. The time in the prompt is by the minute already (`i18n.FormatTime`); a run across a minute's change is simply not taken |
| 3 | A tool call that does not only read | The run stops at the round that calls it and keeps the round. Taken over, the turn runs that round's calls itself, as always, and goes on from there with its own rounds |
| 4 | Which tools only read | Only a tool marked so (`tools.Spec.ReadOnly`: an MCP tool's `readOnlyHint`, `read_only_tools`, an own tool marked read-only). Not the rights' classes: Mira's own tools count as `ReadHouse` there even when they write |
| 5 | Turns that take something | No run while the conversation has a confirmation waiting, a note of an interrupted answer waiting, a thread the turn would run in, or a history long enough to be compacted: a run cannot build their request without taking or changing them |
| 6 | The model's places | A run takes a place only when one is free, never waits for one. A turn that waits for a place ends the runs on its model that hold one |
| 7 | Setting | `speculation.events_url` in `mira.yaml`; off without it. Not on Mira's page: it is for a few setups with such a speech service |

## What it does

### 1. The side channel

```yaml
speculation:
  events_url: http://<speech host>:10312/api/events
```

Mira subscribes to the URL (Server-Sent Events) at start and keeps the subscription. The events
(`ha-voice-rocm/docs/side-channel.md`) carry the side channel's own turn id:

| Event | What Mira does |
|---|---|
| `turn.started` | Notes the side-channel turn with the satellite in `listening` now (decision 1), or with none |
| `reading` with `complete` ≥ 0.5 | Starts a run on its `text`, ending the turn's earlier run |
| `reading` with `complete` < 0.5 or `null` | Nothing: the user may go on |
| `reading.stale` | Ends the run of that reading |
| `turn.final` | When no run has its `text`: starts one. Then keeps the side-channel turn for 10 s (below) |
| `speech.started`, `speech.paused`, others | Nothing (an unknown event is ignored, decision of [Versions](versions.md)) |

0.5 is Smart Turn's own threshold; at most one run per side-channel turn at a time.

### 2. A speculative run

Built as `Handle` and `turn` build a turn, with what Home Assistant will most likely send:

- **Device:** the satellite of decision 1. **Text:** the reading's.
- **Conversation id:** the one Mira's last answer at this satellite left listening (a follow-up), else a new one.
  An anonymous new turn's history is empty either way.
- **Language:** the one Home Assistant sent with this satellite's last turn (Mira notes it per device, in memory);
  the language rule reads it without remembering it.
- **Person:** decision 1; Mira notes per device the person the voice named in its last turn, in memory.

Then:

- **Read, not taken or written:** the session is read, not opened (none or an expired one: an empty history); the
  confirmation, the interruption note and the thread are looked at, not taken; any of them there, or a history due for
  compaction: no run (decision 5), with a debug line naming which.
- **Rounds** as in `turn`, without streaming, on the turn's model; not on a fallback (a failing model ends the run).
  Each round keeps the hash of its request, the model's message, and for each read-only call its result.
- **Tools:** a call of a tool marked read-only passes the guard and runs as in a turn; the first call that is not ends
  the run after this round (decision 3), and no call of that round runs. A guard that does not allow a read-only call
  ends the run the same way.
- **Stored:** nothing. No chat messages, no session, no run row, no `heard` mark, no follow-up note.

The run lives until it is ended (a stale reading, a newer reading, a turn waiting for its place) or 10 s after the
side channel's `turn.final`, whichever is first. Taken over, it is the turn's until the turn is done.

### 3. The turn takes over

In `turn`, before each model round:

1. Its request's hash is compared with the round of the same number of every run kept for its device.
2. **The same:** the turn takes the run's message. If the run's round is still on its way, the turn waits for it
   instead of asking the model a second time. The message's text goes to the stream at once; its read-only calls
   take the run's results; its other calls run now, as in every turn.
3. **Not the same, or no run:** the turn calls the model as always; the runs of its device are ended.

From the first round not taken on, the turn is a normal turn. Everything a turn stores, it stores as today.

One log line per run: `speculation` with the side-channel turn, the device, the text's start, how it ended
(`taken` with the rounds taken and the time the turn waited for them, `stale`, `replaced`, `no_match` with the round
that differed, `expired`, `no_place`, `skipped` with the reason of decision 5, `failed` with the model's error).

### 4. What the model is told

Nothing new: a round taken over is the round the model would have been asked for, with the same request.

### Cost

- **Per side-channel turn:** one subscription event each, no call to Home Assistant.
- **Per run:** the reads a turn makes before its first round (session, history, profile, memory search, device
  lines, structure: all in memory or the database), one model request per round, and the read-only tools it calls
  (the same calls the turn would make).
- **Paid for nothing:** a run not taken. With a cloud model each costs the prompt tokens of a turn (and the tokens
  written until it was ended). On the 241 turns: a run per pause with `complete` ≥ 0.5, so about one to two runs per
  turn; Mira's log line per run counts them.
- **The turn waits for:** a hash of its request per round (microseconds); for a round still on its way, its rest.
- **Grows with:** the number of satellites listening at once (one run each at most).

### Fails

| What | Who learns of it | What happens |
|---|---|---|
| The side channel cannot be reached, the connection breaks, a 5xx | a warning at the first failure and when the error changes, info when connected again | reconnect, exponential from 1 s to 60 s, never given up (row 5 of [Retries](retry.md)'s table, for the side channel); no runs meanwhile, turns as always |
| The URL answers 404 or not `text/event-stream` | an error naming the URL and the answer's start | no runs until the setting changes; turns as always |
| An event that does not parse | a warning with the event's start | the event is left out; the next one counts |
| No satellite or several in `listening` | debug | no run for that side-channel turn |
| No free place at the model | debug | no run |
| The model fails in a run | `speculation … failed` with the error | the run ends; the turn calls the model as always and finds the error itself |
| A read-only tool fails in a run | its result says so, as in a turn | the round is kept with that result; the turn's request is then the same only if its tool would fail the same way, which it compares as everything else |
| The turn's request differs | `speculation … no_match`, the round | the turn calls the model; nothing was done twice |
| A turn comes for a device that has no run | nothing | as always |

The worst case is a run that read a state a second before the turn ("the light is off") and is taken over: the same
as a turn that read it a second earlier.

## Steps

1. **The side-channel client** (`internal/sidechannel`): setting, subscription with reconnect, the events as types,
   the side-channel turn with its satellite; a fake side channel in the tests (a recorded stream of the reference
   server). SC-01 to SC-05 (built 2026-11-25).
2. **The run** (`conversation`): building a turn's request without changing state, the rounds with read-only tools,
   the end of a run. SC-06 to SC-11.
3. **Taking over** in `turn`, round by round, with waiting for a round on its way and the model's places. SC-12 to
   SC-17.
4. **Through Mira on the test HA**: a fake side channel in `testenv` driven together with `techo5_fake.say`, every
   scenario once end to end; the log lines.
5. **At home** (the maintainer): the reference server with `ASR_SPECULATE=1`, a week of turns; the share of runs
   taken and the time saved from the log lines.
