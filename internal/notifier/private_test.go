package notifier

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/choices"
	"github.com/miraassist/mira-app/internal/config"
	haws "github.com/miraassist/mira-app/internal/ha/ws"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
)

// Private messages: docs/spec/schedule-receiver.md, step 1.

func private(receiver string) func(*store.Message) {
	return func(n *store.Message) {
		n.Confidentiality = store.ConfidentialityPrivate
		n.Receiver = receiver
	}
}

func TestPS01_APrivateMessageGoesOutOverTheChannelsAtTheTick(t *testing.T) {
	e := newEnv(t, nil)
	e.message("posts", 1, private("ben"))
	e.clk.Set(at("08:00"))
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if got := e.tg.sent["notify.ben"][0]; !strings.Contains(got, "Hinweis posts") || !strings.Contains(got, "Details posts") {
		t.Errorf("sent text = %q, want spoken text and details", got)
	}
	if st := e.state("posts"); st != store.MessageDelivered {
		t.Errorf("state = %q, want delivered", st)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("a private message was spoken: %+v", e.ha.calls)
	}
}

func TestPS02_APrivateMessageIsNeverSpoken(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Notifier.ConfidentialVoice = config.ConfidentialVoiceWhenAlone })
	e.message("posts", 2, private("ben"))
	e.clk.Set(at("08:00"))
	ctx := context.Background()
	if w, err := e.n.waiting(ctx, "bad"); err != nil || len(w) != 0 {
		t.Fatalf("a private message counts as waiting: %v %v", w, err)
	}
	out := e.presence("bad")
	if len(out.Announced) != 0 || len(e.ha.calls) != 0 {
		t.Fatalf("spoken: %+v %+v", out, e.ha.calls)
	}
	if len(out.Private) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("not sent privately: %+v %v", out, e.tg.sent)
	}
}

func TestPS03_APrivateMessageWaitsForTheEndOfQuietHours(t *testing.T) {
	e := newEnv(t, nil) // 03:00, inside quiet hours
	e.message("posts", 1, private("ben"))
	if sent := e.tick(); len(sent) != 0 || len(e.tg.sent) != 0 {
		t.Fatalf("sent in quiet hours: %v %v", sent, e.tg.sent)
	}
}

func TestPS04_AnUrgentPrivateMessageIsNotAnnounced(t *testing.T) {
	e := newEnv(t, nil)
	e.message("posts", 3, private("ben"))
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("an urgent private message was announced: %+v", e.ha.calls)
	}
}

// A private message of the house goes to every person with a channel and
// skips the persons without one (docs/spec/group-watch.md, decision 20).
func TestAPrivateMessageOfTheHouseGoesToEveryPersonWithAChannel(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna"}}
		c.Persons["martin"] = config.Person{Entity: "person.martin"}
	})
	e.message("fenster", 2, private(store.ReceiverHouse))
	e.clk.Set(at("08:00"))
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 || len(e.tg.sent["notify.anna"]) != 1 {
		t.Fatalf("sent = %v, channels = %v", sent, e.tg.sent)
	}
	if strings.Contains(e.logged(), "partly failed") {
		t.Errorf("a person without a channel counted as failed: %s", e.logged())
	}
}

// PS-09 A private message to a Telegram chat keeps its Markdown and parses
// (docs/spec/channels.md, Telegram's Markdown)
func TestPS09_APrivateMessageToATelegramChatKeepsItsMarkdownAndParses(t *testing.T) {
	// Given a person whose channel is a notify entity of telegram_bot, and one with the companion app
	e := newEnv(t, func(c *config.Config) {
		c.Persons["ben"] = config.Person{Entity: "person.ben", Channels: []string{"notify.telegram_ben"}}
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna_phone"}}
	})
	e.ha.set("notify.telegram_ben", "unknown", nil)
	e.ha.set("notify.anna_phone", "unknown", nil)
	e.ha.set("person.anna", "home", nil)
	e.ha.platforms = map[string]string{"notify.telegram_ben": "telegram_bot", "notify.anna_phone": "mobile_app"}
	// When a private message with an address with "_", a name like max_muster and "_kursiv_" goes out
	text := func(n *store.Message) {
		n.Spoken = "YouTube: _kursiv_ von max_muster."
		n.Details = "• Kanal: Video (11 min). https://www.youtube.com/watch?v=wSFJwZ9_fGg"
	}
	e.message("ben-list", 1, func(n *store.Message) { private("ben")(n); text(n) })
	e.message("anna-list", 1, func(n *store.Message) { private("anna")(n); text(n) })
	e.clk.Set(at("08:00"))
	e.tick()
	// Then the Telegram chat gets the "_" of the address and the name escaped and "_kursiv_" as it is
	tg := strings.Join(e.tg.sent["notify.telegram_ben"], "\n")
	if !strings.Contains(tg, `watch?v=wSFJwZ9\_fGg`) || !strings.Contains(tg, `max\_muster`) || !strings.Contains(tg, "_kursiv_") ||
		strings.Contains(tg, `\_kursiv`) {
		t.Errorf("telegram got %q", tg)
	}
	// And the companion app gets the text unchanged
	app := strings.Join(e.tg.sent["notify.anna_phone"], "\n")
	if !strings.Contains(app, "wSFJwZ9_fGg") || strings.Contains(app, `\`) {
		t.Errorf("app got %q", app)
	}
}

// PS-10 A message Telegram cannot parse is not tried again
func TestPS10_AMessageTelegramCannotParseIsNotTriedAgain(t *testing.T) {
	// Given a channel that refuses the message with "Can't parse entities"
	e := newEnv(t, nil)
	logs := e.logs()
	e.tg.fail = map[string]error{"notify.ben": &haws.Error{Command: "call_service", Code: "unknown_error",
		Message: "Can't parse entities: can't find end of the entity starting at byte offset 221"}}
	e.message("posts", 1, private("ben"))
	// When the private delivery runs
	e.clk.Set(at("08:00"))
	e.tick()
	// Then the message is failed at once, after one attempt, with a log line
	if st := e.state("posts"); st != store.MessageFailed || e.tg.tries["notify.ben"] != 1 {
		t.Fatalf("state = %q after %d attempts", st, e.tg.tries["notify.ben"])
	}
	if out := logs.String(); !strings.Contains(out, `msg="private delivery failed"`) || !strings.Contains(out, "fatal=true") {
		t.Errorf("log = %s", out)
	}
}

func TestTelegramMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"*fett* und _kursiv_ und `code`":        "*fett* und _kursiv_ und `code`",
		"ein einzelnes * Sternchen":             `ein einzelnes \* Sternchen`,
		"[Text](https://a.example/x_y) und a_b": `[Text](https://a.example/x_y) und a\_b`,
		"Liste [1 von 3":                        `Liste \[1 von 3`,
		"schon \\_ maskiert":                    "schon \\_ maskiert",
		"snake_case und _kursiv_ und 3*4":       `snake\_case und _kursiv_ und 3\*4`,
	} {
		if got := TelegramMarkdown(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// PC-01 A thread waiting for a yes asks privately with buttons
// (docs/spec/private-choices.md, decisions 2 to 5).
func TestPC01_AThreadWaitingForAYesAsksPrivatelyWithButtons(t *testing.T) {
	// Given a thread of Anna waiting for her yes is asked over her Telegram chat
	e := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.telegram_anna"}}}
	})
	e.ha.set("notify.telegram_anna", "unknown", nil)
	e.ha.mu.Lock()
	if e.ha.platforms == nil {
		e.ha.platforms = map[string]string{}
	}
	e.ha.platforms["notify.telegram_anna"] = "telegram_bot"
	e.ha.mu.Unlock()
	e.n.asks = func(_ context.Context, id string) ([]string, bool) { return nil, id == "th1" }
	e.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e.clk.Set(at("08:00"))
	e.tick()
	// Then the message has the buttons Ja and Nein, their data /mira <id> yes and /mira <id> no
	var sent *call
	for i, c := range e.ha.calls {
		if c.Domain == "telegram_bot" && c.Service == "send_message" {
			sent = &e.ha.calls[i]
		}
	}
	if sent == nil || sent.Data["entity_id"] != "notify.telegram_anna" ||
		!slices.Equal(sent.Data["inline_keyboard"].([]string), []string{"Ja:/mira th1 yes,Nein:/mira th1 no"}) {
		t.Fatalf("calls %+v, private %v", e.ha.calls, e.tg.sent)
	}
	// Over the companion app: the actions, with a typed answer as the third.
	e2 := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.mobile_app_annas_phone"}}}
	})
	e2.n.asks = func(_ context.Context, id string) ([]string, bool) { return nil, id == "th1" }
	e2.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e2.clk.Set(at("08:00"))
	e2.tick()
	data := e2.tg.data["notify.mobile_app_annas_phone"]
	if len(data) != 1 || data[0]["tag"] != "mira-th1" {
		t.Fatalf("data %+v", data)
	}
	acts := data[0]["actions"].([]map[string]any)
	if len(acts) != 3 || acts[0]["action"] != "mira:th1:yes" || acts[1]["title"] != "Nein" || acts[2]["behavior"] != "textInput" {
		t.Errorf("actions %+v", acts)
	}
	// A message of a thread that waits for no yes gets none.
	e3 := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.mobile_app_annas_phone"}}}
	})
	e3.n.asks = func(context.Context, string) ([]string, bool) { return nil, false }
	e3.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e3.clk.Set(at("08:00"))
	e3.tick()
	if len(e3.tg.data) != 0 || len(e3.tg.sent["notify.mobile_app_annas_phone"]) != 1 {
		t.Errorf("without a question: data %+v sent %v", e3.tg.data, e3.tg.sent)
	}
}

// PC-02 Proposals become buttons
func TestPC02_ProposalsBecomeButtons(t *testing.T) {
	props := []string{"Donnerstag passt.", "Lieber Freitag.", "Ich melde mich."}
	// Given a thread waiting for a choice among three proposals is asked over Anna's companion app
	e := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.mobile_app_annas_phone"}}}
	})
	e.n.asks = func(context.Context, string) ([]string, bool) { return props, true }
	e.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e.clk.Set(at("08:00"))
	e.tick()
	// Then the notification has actions for the proposals and one to answer in words
	data := e.tg.data["notify.mobile_app_annas_phone"]
	if len(data) != 1 {
		t.Fatalf("data %+v", data)
	}
	acts := data[0]["actions"].([]map[string]any)
	v := choices.Version(props)
	if len(acts) != 3 || acts[0]["action"] != "mira:th1:1."+v || acts[1]["title"] != "Vorschlag 2" || acts[2]["behavior"] != "textInput" {
		t.Errorf("actions %+v", acts)
	}
	if got := e.tg.sent["notify.mobile_app_annas_phone"][0]; !strings.Contains(got, "\n1. Donnerstag passt.\n2. Lieber Freitag.\n3. Ich melde mich.") {
		t.Errorf("text %q", got)
	}
	// In Telegram: a button per proposal, and Nein.
	e2 := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.telegram_anna"}}}
	})
	e2.ha.set("notify.telegram_anna", "unknown", nil)
	e2.ha.mu.Lock()
	if e2.ha.platforms == nil {
		e2.ha.platforms = map[string]string{}
	}
	e2.ha.platforms["notify.telegram_anna"] = "telegram_bot"
	e2.ha.mu.Unlock()
	e2.n.asks = func(context.Context, string) ([]string, bool) { return props, true }
	e2.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e2.clk.Set(at("08:00"))
	e2.tick()
	var kb []string
	for _, c := range e2.ha.calls {
		if c.Domain == "telegram_bot" {
			kb = c.Data["inline_keyboard"].([]string)
		}
	}
	// One row per button since decision 16; each proposal's with the mark of its proposals (decision 22).
	want := []string{"Vorschlag 1:/mira th1 1." + v, "Vorschlag 2:/mira th1 2." + v, "Vorschlag 3:/mira th1 3." + v, "Nein:/mira th1 no"}
	if !slices.Equal(kb, want) {
		t.Errorf("keyboard %q", kb)
	}
}

// PC-12 The target names its buttons in the person's language
func TestPC12_TheTargetNamesItsButtonsInThePersonsLanguage(t *testing.T) {
	props := []string{"Donnerstag passt.", "Lieber Freitag.", "Ich melde mich."}
	target := &skills.Target{Labels: map[string]skills.ChoiceLabels{
		"de": {Choose: "Vorschlag {n} senden", Other: "Was anderes senden", Dismiss: "Ignorieren", Ask: "Was soll ich stattdessen antworten?"},
		"en": {Choose: "Send proposal {n}", Other: "Send something else", Dismiss: "Ignore"}}}
	labels := func(_ context.Context, _ string, lang i18n.Lang) choices.Labels {
		return choices.ForTarget(target, lang)
	}
	v := choices.Version(props)
	// Given Anna, who speaks German, is asked privately with three proposals in Telegram
	e := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"anna": {Entity: "person.anna", Channels: []string{"notify.telegram_anna"}}}
	})
	e.ha.set("notify.telegram_anna", "unknown", nil)
	e.ha.mu.Lock()
	if e.ha.platforms == nil {
		e.ha.platforms = map[string]string{}
	}
	e.ha.platforms["notify.telegram_anna"] = "telegram_bot"
	e.ha.mu.Unlock()
	e.n.asks = func(context.Context, string) ([]string, bool) { return props, true }
	e.n.labels = labels
	e.message("q", 1, func(m *store.Message) { private("anna")(m); m.ThreadID = "th1" })
	e.clk.Set(at("08:00"))
	e.tick()
	var kb []string
	for _, c := range e.ha.calls {
		if c.Domain == "telegram_bot" {
			kb = c.Data["inline_keyboard"].([]string)
		}
	}
	// Then Telegram shows one row each
	want := []string{"Vorschlag 1 senden:/mira th1 1." + v, "Vorschlag 2 senden:/mira th1 2." + v, "Vorschlag 3 senden:/mira th1 3." + v,
		"Was anderes senden:/mira th1 other", "Ignorieren:/mira th1 no"}
	if !slices.Equal(kb, want) {
		t.Errorf("keyboard %q", kb)
	}
	// And the app shows the first proposal, "other" with a text field, "dismiss"
	acts := choices.AppActions("th1", labels(context.Background(), "th1", i18n.DE), props)
	if len(acts) != 3 || acts[0]["title"] != "Vorschlag 1 senden" || acts[1]["action"] != "mira:th1:other" ||
		acts[1]["behavior"] != "textInput" || acts[2]["title"] != "Ignorieren" || acts[2]["action"] != "mira:th1:no" {
		t.Errorf("actions %+v", acts)
	}
	// In English the target's English titles; a target without labels keeps Mira's own.
	if l := choices.ForTarget(target, i18n.EN); l.Proposal != "Send proposal {n}" || l.Ask != "What should I send instead?" {
		t.Errorf("en %+v", l)
	}
	if kb := choices.TelegramKeyboard("th1", choices.ForTarget(&skills.Target{}, i18n.DE), nil); !slices.Equal(kb, []string{"Ja:/mira th1 yes,Nein:/mira th1 no"}) {
		t.Errorf("without labels %q", kb)
	}
}
