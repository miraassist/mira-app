# Departures (skill `fahrplan`) – Spec

Status 2026-11-26: decided (the maintainer: 1 and 3 to 6 as proposed, 2B); steps 1 and 2 built, step 3 run against dbf
(the run through the test Mira is open while the test HA is busy). Scenarios:
[fahrplan.feature](fahrplan.feature) (FP-01 to FP-10).

## Goal

"Wann geht der nächste Bus?" gets the next departures at the house's stop: line, destination, minutes, a delay or a
cancellation. Reported 2026-11-26 by a user with the stop "Pulverturm" in Oldenburg and the network "VWG": no useful
answer.

What was found (2026-11-26, by asking dbf.finalrewind.org directly as the skill does):

1. **The model sees almost nothing of the answer.** The skill reads dbf's JSON with `web_page`, which gives a model at
   most 6000 characters of a page (`maxPageChars`, internal/websearch). dbf's answer for 30 departures is 448 KB for
   Oldenburg Pulverturm with `efa`, 188 KB for München Marienplatz (`efa=MVV`), 18 KB with `hafas=VBN`, 34 KB for
   Karlsruhe Hbf (trains): every departure carries its whole route. In the first 6000 characters there is not one
   complete departure; the model gets the first departure's line, destination and minutes, then stops of its route.
2. **"VWG" is no network dbf knows.** dbf knows 21 EFA networks (`efa=`: AVV, BEG, BSVG, DING, KVV, LinzAG, MVV, NVBW,
   NWL, RVV, Rolph, VAG, VGN, VMV, VRN, VRR, VRR2, VRR3, VVO, VVS, bwegt) and 25 HAFAS networks (`hafas=`: AVV, BART,
   BLS, BVG, CFL, CMTA, DSB, IE, KVB, NAHSH, NASA, NVV, PKP, RMV, RSAG, Resrobot, STV, SaarVV, TPG, VBB, VBN, VMT, VOS,
   ZVV, mobiliteit), from its page `/_backend`. An unknown `efa` value is not refused: dbf takes a default; a unique
   name happens to work (Oldenburg Pulverturm: only the lines 330 and 910), an ambiguous one lands elsewhere
   ("Hauptbahnhof" with `efa=VWG`: Erfurt). Oldenburg is in the VBN, which dbf has as `hafas=VBN` only; the skill
   knows only `efa`. With `hafas=VBN` and the name "Oldenburg(Oldb) Pulverturm": the lines 304, 314, 320, 322, 324,
   380 with real-time delays.
3. **Names are strict, and the failures differ.** `efa`: "Pulverturm" alone gives an empty list, without an error;
   an ambiguous name `500 "Backend error: name list"`, an unknown one `500 "Backend error: stop invalid"`. `hafas`:
   a name the network does not know as written `300 "svcResL[0].err is LOCATION"` ("Oldenburg Pulverturm" and
   "Pulverturm, Oldenburg" both fail for VBN). Trains: an unknown name `300 "Ambiguous station name"`.
4. **Three formats.** `efa`: `type`, `line`, `destination`, `countdown` (minutes), `delay`, `is_cancelled`,
   `platform_name`. `hafas`: `train` ("Bus 314"), `destination`, `scheduledTime` and `time` (Unix seconds), `delay`,
   `isCancelled`, `platform`, `messages`. Trains: `train`, `destination`, `scheduledDeparture` ("HH:MM", empty when the
   train ends here), `delayDeparture`, `isCancelled`, `platform`, `via`, `messages`.

## Building blocks

A skill with a Starlark tool and a web API without a login ([API tools](api-tools.md), decisions 8 and 16), as
[`boerse`](boerse.md): the tool fetches dbf's answer and gives the model a short list; the skill's text says how to
answer. The code passes data, the model only words it. Not chosen: `web_page` shortening
large JSON for every skill (it cannot know which part of an unknown structure matters, and would cut silently); only
changing the skill's text (even three departures of `efa` are far above 6000 characters).

## Decisions (2026-11-26, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Which networks | The setting `verbund` takes a network as dbf names it, EFA or HAFAS; the tool knows both lists (above) and asks with `efa=` or `hafas=`. Empty: trains only, as now |
| 2 | An unknown network ("VWG") | The tool fails at the call with the known networks, and Mira says it (2B). Not: a new building block `enum` for skill settings with a problem at the start (2A, more than this case needs); not: a default network (that is what dbf does, with wrong answers) |
| 3 | A stop that is not found (empty `efa` list, "name list", "stop invalid", "LOCATION", "Ambiguous station name") | The tool's error says what was asked and what to do: the name as the network writes it (for VBN, say, "Oldenburg(Oldb) Pulverturm", as in the network's app or timetable). Mira says it and offers no departures. No guessing of other spellings |
| 4 | What the tool returns | At most 20 departures from now on, each: `art` (Bus, Straßenbahn, S-Bahn, RE, ICE …), `linie`, `ziel`, `in_minuten`, `uhrzeit` (HH:MM, the planned time), `verspaetung` (minutes, missing without real time), `ausfall` (only when cancelled), `gleis`; for trains also `ueber` (up to four stops on the way) and `hinweise` (up to two reasons for a delay). Plus `haltestelle` (as asked) and `netz`. No routes |
| 5 | "… nach Stuttgart?" | The tool takes an optional `richtung`; it keeps departures whose `ziel` or `ueber` contains it (ignoring case). Nothing left → an empty list with a sentence that no departure goes there among the next ones. Code filters, the model words |
| 6 | Another stop than the house's | The tool takes an optional `haltestelle`; without it the setting's. The network stays the setting's |

## What it does, step by step

1. The model calls `fahrplan__abfahrten(haltestelle?, richtung?)`.
2. The tool takes the stop (the argument or the setting) and the network (the setting): `efa=<net>` or `hafas=<net>`
   from the lists, none for trains; an unknown network fails (decision 2).
3. One `GET https://dbf.finalrewind.org/<name>.json?<efa|hafas>=<net>&limit=30` (the name URL-encoded).
4. The answer is read by its format (decision 4), departures already gone are left out, `richtung` filters (decision
   5), at most 20 go back.
5. The model answers as the skill's text says: the next fitting departure with line, destination and minutes, a
   delay or cancellation first; more on request.

**Fails:**
- The network is unknown → the tool's error with the known networks (decision 2), without asking dbf.
- The stop is not found (point 3 above) → the tool's error with what was asked and how the name must be written
  (decision 3).
- dbf answers another status or no JSON, or does not answer within 20 s → the tool's error with the status and the
  answer's start (as `boerse`), one log line of the tool; Mira says she cannot reach the timetable now. No retry: a
  `500` of dbf is also its answer for a wrong name, so a status does not tell a passing failure from a lasting one,
  and a question at a satellite does not wait for a second try.
- A departure whose fields do not read (a type Mira does not expect) is left out with a log line; the others go back
  ([Reading what comes in](parsing.md)). All left out → the tool's error that dbf's answer could not be read, with its
  start.

## What it costs

One request to dbf per question, at most 20 s, which the turn waits for; dbf's answer is downloaded whole (up to about
450 KB for 30 departures of `efa`), the model gets at most 20 short entries. No key, nothing stored. dbf is a free
service of a private person, unofficial: it may change or refuse; then the tool's error says so.

## Steps

1. **The tool** `abfahrten` in `skills/fahrplan/tools.star`, the `api` section for dbf, the three formats, the
   networks' lists, the errors. FP-01 to FP-08 in Go, against a fake
   dbf with the answers recorded on 2026-11-26.
2. **The skill's text and settings**: the instructions to use the tool, the setting `verbund` with examples ("VBN für
   Bremen und Oldenburg", "KVV", "MVV") and `haltestelle` with the name as the network writes it; the guide (de, en).
   FP-09, FP-10.
3. **Run** against dbf itself with the reported stop (VBN, "Oldenburg(Oldb) Pulverturm"), a train station and an
   unknown network; through Mira's conversation API on the test Mira when the test HA is free.
   Run 2026-11-26 against dbf: VBN "Oldenburg(Oldb) Pulverturm" gave 20 departures in 2346 characters (the lines 304,
   311, 314, 315, 321, 322, 324, 380, delays, one cancellation), with `richtung` "ZOB" only those; "Karlsruhe Hbf"
   trains with stops on the way; "VWG" the error with the known networks; "Oldenburg Pulverturm" with VBN the error
   saying how to write the stop. Seen: for trains `art` is the train's first word, for the regional trains of Karlsruhe
   the operator ("AVG" of "AVG S7"); the line says the rest.
