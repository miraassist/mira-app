# Choices in a private question (buttons) – Spec

Status 2026-11-24: decided (decision 1 by the maintainer, 2 to 12 taken while writing); the state table checked on
the test HA (2026-11-24, the test HA's Telegram bot and the maintainer's phone); step 2 built, steps 3 and 4 not. Scenarios: [private-choices.feature](private-choices.feature) (PC-01 to
PC-08).

## Goal

Mira asks a person privately: over their channel, Telegram or the Home Assistant companion app, not at a speaker
others may hear. Today the person answers by writing back (Telegram) or by speaking to the app's Assist. A quick
answer should be a tap: "Ja" or "Nein", or one of up to three proposals. A changed answer is still written or spoken.

## Building blocks

- **A person's channels** ([Channels](channels.md), [Messages](messages.md)): private messages go over the person's
  Home Assistant notify targets.
- **A thread bound to the person** ([Threads](threads.md)): it follows the person to every satellite and into
  Telegram; an answer there lands in the same thread.
- **A pending confirmation** ([Hooks](hooks.md), `confirm`): a call parked until the user's yes.

New is only the tap: buttons on the private message, and the tap as the answer.

## Decisions

### By the maintainer (2026-11-24)

| # | Question | Decision |
|---|---|---|
| 1 | How a person answers a private question | Both: buttons for the quick answer, a message (Telegram chat, the app's Assist) for a changed one |

### Taken while writing (2026-11-24)

| # | Question | Decision |
|---|---|---|
| 2 | Which questions get buttons | A thread waiting for a yes (its buttons "Ja" and "Nein"), and a thread waiting for a choice among proposals (one button per proposal, at most three, and "Nein"); asked over a private channel only. Every other private message stays without buttons |
| 3 | Telegram | `telegram_bot.send_message` with `inline_keyboard` (one row per button, "Label:/data", no space after a comma: it goes into the next label); the data **must begin with "/"**: without it Home Assistant takes the whole pair as an upper-cased label (seen 2026-11-24: "Vorschlag 2:mira:t1:2" became the label "ORSCHLAG 2:MIRA:T1:2"). The tap arrives as the state of the bot's event entity with `event_type` `telegram_callback`: `data`, `command` and `args` (the data split at spaces), `chat_id`, `user_id`, `id` (a string), `message.message_id`. Mira answers it with `telegram_bot.answer_callback_query` **at once** (minutes later Telegram refuses: "Query is too old") and removes the buttons with `telegram_bot.edit_replymarkup` (an empty list); that edit fires a `telegram_sent` state of the same entity, which Mira passes over |
| 4 | The companion app | `notify.mobile_app_<device>` with `data.actions` (`action`, `title`; one more with `behavior: textInput` for a typed answer) and a `tag`; the tap arrives as the event `mobile_app_notification_action` with `action`, `reply_text` (for the typed answer), `tag`, every action's key and title, `device_id`, and in its `context` the `user_id` of the app's Home Assistant user (seen 2026-11-24 on an Android phone). Android shows three actions: so at most three proposals, and "Nein" and the typed answer go together into "Antworten" (a typed "nein" is a no). The buttons show when the notification is expanded; a tap on the notification itself only opens the app |
| 5 | Tying a tap to its question | `<id>`, a short id of the question Mira keeps (12 characters), and `<choice>`, `yes`, `no` or `1` to `3`. In Telegram the data is `/mira <id> <choice>` (decision 3: it must begin with "/"; it comes back as `command` `/mira` and `args`); in the app the action is `mira:<id>:<choice>`. Telegram allows 64 bytes of data (its Bot API); the companion app's docs advise an id in the action, since actions are shared across notifications |
| 6 | Who may answer | Only the person asked: in Telegram the chat the question went to, in the app a tap whose event's `context.user_id` is the person's Home Assistant user. Another tap: refused, a log line |
| 7 | What a tap does | Code, no model: "Ja" confirms the thread (`thread_confirm`'s path); a proposal fills the thread's field with it and confirms; "Nein" ends the thread as declined. The same as the person's typed "ja", "nein" or "den zweiten", only without a turn |
| 8 | A tap on a pending confirmation | When the private question is a pending confirmation (a tool under `confirm`), the tap is its yes, wherever the call was parked: the pending confirmation carries the question's id and is found by it, not only by the session. The question over the private channel is the confirmation itself, never one before a second |
| 9 | After the tap | Telegram: the callback is answered at once ("Erledigt.", "Abgelehnt."; before the thread's work, which may take longer) and the buttons removed, so a second tap cannot come; the app: the notification stays until the person clears it (removing it is not in the app's docs read) |
| 10 | A tap that comes late | The thread or the pending confirmation is gone (ended, expired): the tap does nothing; Telegram's callback answer says "Das ist schon erledigt."; a log line with the id |
| 11 | How Mira hears taps | Push: a subscription to the Telegram bots' event entities' state changes (filtered to them) and to the event `mobile_app_notification_action`; the handler only passes the tap on (the subscription rule) |
| 12 | A typed answer | Unchanged: the Telegram message or the app's Assist is a turn of the person and finds the thread; the app's "Antworten" text (`reply_text`) is taken as such a turn |

## The state table

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | The question with buttons arrives | Mira's send | Telegram: the send's answer with `message_id`; the app: none | the action's response | 2026-11-24: Telegram's `message_id` |
| 2 | A tap in Telegram | the person | the bot's event entity changes, `event_type` `telegram_callback`, `data` `/mira <id> <choice>` | the state subscription (decision 11) | 2026-11-24, the test HA's bot (`event.mira_ereignis_aktualisieren`): `data` `/mira t1 yes`, `command` `/mira`, `args` [t1, yes], `chat_id`, `user_id`, `id` |
| 3 | A tap in the app | the person | event `mobile_app_notification_action`, `action` `mira:<id>:<choice>` | the event subscription | 2026-11-24, the maintainer's phone: `action` `mira:t4:2`, `tag`, `device_id`, `context.user_id` |
| 4 | A typed answer in the app | the person | the same event with `reply_text` | the event subscription | 2026-11-24: `action` `mira:t6:reply`, `reply_text` "ja das geht" |
| 5 | The buttons go (Telegram) | Mira after the tap | `answer_callback_query` and `edit_replymarkup` succeed when sent at once; the edit fires `telegram_sent` | the actions' responses | 2026-11-24: both succeeded at once; minutes later the answer was refused ("Query is too old") |

Built only when every row is checked.

## Fails

| Step | Failure | Who learns of it |
|---|---|---|
| sending | the channel refuses the buttons (an older bot, a target without actions) | the message goes out without buttons, as today; a log line with the channel and the error. The answer by message still works |
| tap | from someone else (decision 6) | nothing happens; a log line; Telegram's callback answer says "Das ist nicht deine Frage." |
| tap | late (decision 10) | as decision 10 |
| tap | the confirming fails (a finish's error) | the thread's own error path: the person gets the error as the thread's message |
| after | answering the callback or removing the buttons fails | a log line; a second tap is a late one (decision 10) |

## What it costs

A question: one notify or Telegram call, as today. A tap: one event, then the thread's work as for a typed yes; for
Telegram two more calls (the answer, the buttons removed). Two subscriptions for the service's life. Nothing grows
with the number of questions but the ids of the open ones.

## Steps

1. **The state table** on the test HA: its Telegram bot and the maintainer's phone in the companion app.
2. **Ja and Nein for threads** (decisions 2 to 7, 9 to 12, for a thread waiting for a yes): the buttons on the
   private message, the subscriptions, the person's check, the answer at once in Telegram, the typed answer in the
   app as a turn. PC-01, PC-03, PC-05, PC-06. Built 2026-11-24 (`internal/choices`, the notifier's `sendTo`).
3. **Proposals as buttons** (decisions 2, 7): which field a chosen proposal goes into is still to be decided. PC-02,
   PC-04.
4. **A tap on a pending confirmation** (decision 8). PC-07, PC-08.
