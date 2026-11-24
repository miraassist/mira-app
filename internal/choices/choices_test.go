package choices

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/config"
	haws "github.com/miraassist/mira-app/internal/ha/ws"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/testsupport/logcap"
)

type called struct {
	service string
	data    map[string]any
}

type fakeHA struct{ calls []called }

func (f *fakeHA) Subscribe(context.Context, string, haws.EventHandler) (func(), error) {
	return func() {}, nil
}
func (f *fakeHA) SubscribeTrigger(context.Context, map[string]any, haws.EventHandler) (func(), error) {
	return func() {}, nil
}

// Entities: the bot's event entity and Anna's chat, unique id "<bot>_<chat>".
func (f *fakeHA) Entities(context.Context) ([]haws.Entity, error) {
	return []haws.Entity{
		{EntityID: "event.mira_bot_update", Platform: "telegram_bot", UniqueID: "42_update_event"},
		{EntityID: "notify.telegram_anna", Platform: "telegram_bot", UniqueID: "42_1001"},
		{EntityID: "notify.telegram_ben", Platform: "telegram_bot", UniqueID: "42_2002"},
	}, nil
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, data map[string]any, _ *haws.Target) error {
	f.calls = append(f.calls, called{domain + "." + service, data})
	return nil
}

type fakeThreads struct {
	threads              map[string]store.Thread
	confirmed, cancelled []string
	chosen               []string
	notes                []string
	confirmErr           error
}

func (f *fakeThreads) Note(_ context.Context, id, text string) error {
	f.notes = append(f.notes, id+": "+text)
	return nil
}

func (f *fakeThreads) Get(_ context.Context, id string) (store.Thread, error) {
	th, ok := f.threads[id]
	if !ok {
		return store.Thread{}, store.ErrNotFound
	}
	return th, nil
}

func (f *fakeThreads) Confirm(_ context.Context, id string) (string, error) {
	f.confirmed = append(f.confirmed, id)
	return "Eingetragen: Elternabend.", f.confirmErr
}

func (f *fakeThreads) Choose(_ context.Context, id, proposal string) (string, error) {
	f.chosen = append(f.chosen, id+": "+proposal)
	return "Gesendet: " + proposal, nil
}

func (f *fakeThreads) Cancel(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

type bench struct {
	s       *Service
	ha      *fakeHA
	threads *fakeThreads
	told    []string
	turns   []string
	logs    interface{ String() string }
}

func newBench(t *testing.T, state string) *bench {
	t.Helper()
	b := &bench{ha: &fakeHA{}, threads: &fakeThreads{threads: map[string]store.Thread{
		"th1": {ID: "th1", Person: "anna", State: state}}}}
	log, lines := logcap.Logger()
	b.logs = lines
	b.s = New(Options{HA: b.ha, Threads: b.threads, Log: log,
		Persons: func() map[string]config.Person {
			return map[string]config.Person{
				"anna": {UserID: "u-anna", Channels: []string{"notify.telegram_anna", "notify.mobile_app_annas_phone"}},
				"ben":  {UserID: "u-ben", Channels: []string{"notify.telegram_ben"}},
			}
		},
		Tell: func(_ context.Context, person, text string) error {
			b.told = append(b.told, person+": "+text)
			return nil
		},
		Turn: func(_ context.Context, person, user, text string) error {
			b.turns = append(b.turns, person+"/"+user+": "+text)
			return nil
		}})
	return b
}

// telegramTap is the bot's event entity's attributes for a tap, as seen on
// the test HA 2026-11-24.
func telegramTap(data string, chat int64) map[string]any {
	return map[string]any{"event_type": "telegram_callback", "id": "910154", "data": data, "chat_id": float64(chat),
		"user_id": float64(chat), "bot": map[string]any{"id": float64(42)}, "message": map[string]any{"message_id": float64(65)}}
}

func (b *bench) services() []string {
	var out []string
	for _, c := range b.ha.calls {
		out = append(out, c.service)
	}
	return out
}

// PC-03 A tap on Ja confirms the thread without a model
func TestPC03_ATapOnJaConfirmsTheThreadWithoutAModel(t *testing.T) {
	b := newBench(t, store.ThreadProposed)
	tap, ok := ParseTelegram(telegramTap("/mira th1 yes", 1001))
	if !ok {
		t.Fatal("the tap does not parse")
	}
	// When Anna taps Ja in Telegram
	b.s.Handle(context.Background(), tap)
	// Then the thread is confirmed as by her typed yes, the callback is answered and the buttons are gone
	if strings.Join(b.threads.confirmed, ",") != "th1" {
		t.Fatalf("confirmed %v", b.threads.confirmed)
	}
	if got := strings.Join(b.services(), ","); got != "telegram_bot.answer_callback_query,telegram_bot.edit_replymarkup" {
		t.Errorf("calls %s", got)
	}
	if b.ha.calls[0].data["callback_query_id"] != "910154" || b.ha.calls[1].data["message_id"] != int64(65) {
		t.Errorf("calls %+v", b.ha.calls)
	}
	if len(b.told) != 1 || b.told[0] != "anna: Eingetragen: Elternabend." {
		t.Errorf("told %v", b.told)
	}
	// Nein ends the thread; in the app no answer to Telegram.
	b2 := newBench(t, store.ThreadProposed)
	raw, _ := json.Marshal(map[string]any{"action": "mira:th1:no"})
	tap, ok = ParseApp(raw, "u-anna")
	if !ok {
		t.Fatal("the app's tap does not parse")
	}
	b2.s.Handle(context.Background(), tap)
	if strings.Join(b2.threads.cancelled, ",") != "th1" || len(b2.ha.calls) != 0 {
		t.Errorf("cancelled %v, calls %v", b2.threads.cancelled, b2.services())
	}
}

// PC-05 Only the person asked may tap
func TestPC05_OnlyThePersonAskedMayTap(t *testing.T) {
	// When a tap for Anna's question comes from another chat or another Home Assistant user
	b := newBench(t, store.ThreadProposed)
	tap, _ := ParseTelegram(telegramTap("/mira th1 yes", 2002))
	b.s.Handle(context.Background(), tap)
	raw, _ := json.Marshal(map[string]any{"action": "mira:th1:yes"})
	app, _ := ParseApp(raw, "u-ben")
	b.s.Handle(context.Background(), app)
	// Then nothing happens and the log names it
	if len(b.threads.confirmed) != 0 || len(b.told) != 0 {
		t.Fatalf("confirmed %v told %v", b.threads.confirmed, b.told)
	}
	if strings.Count(b.logs.String(), "a tap refused") != 2 {
		t.Errorf("log:\n%s", b.logs.String())
	}
	if len(b.ha.calls) < 1 || b.ha.calls[0].data["message"] != "Das ist nicht deine Frage." {
		t.Errorf("calls %+v", b.ha.calls)
	}
}

// PC-06 A late tap does nothing
func TestPC06_ALateTapDoesNothing(t *testing.T) {
	// Given Anna's thread has ended
	b := newBench(t, store.ThreadDone)
	tap, _ := ParseTelegram(telegramTap("/mira th1 yes", 1001))
	// When she taps Ja
	b.s.Handle(context.Background(), tap)
	// Then nothing happens and Telegram's callback answer says it is done already
	if len(b.threads.confirmed) != 0 || b.ha.calls[0].data["message"] != "Das ist schon erledigt." {
		t.Fatalf("confirmed %v calls %+v", b.threads.confirmed, b.ha.calls)
	}
	// A thread that is gone altogether: the same.
	gone, _ := ParseTelegram(telegramTap("/mira weg yes", 1001))
	b.s.Handle(context.Background(), gone)
	if len(b.threads.confirmed) != 0 {
		t.Errorf("confirmed %v", b.threads.confirmed)
	}
}

// The app's typed answer is a turn of the person (decision 12); a failing
// yes is told with its error.
func TestATypedAnswerIsATurnAndAFailingYesIsTold(t *testing.T) {
	b := newBench(t, store.ThreadProposed)
	raw, _ := json.Marshal(map[string]any{"action": "mira:th1:reply", "reply_text": "ja das geht"})
	tap, ok := ParseApp(raw, "u-anna")
	if !ok {
		t.Fatal("the typed answer does not parse")
	}
	b.s.Handle(context.Background(), tap)
	if len(b.turns) != 1 || b.turns[0] != "anna/u-anna: ja das geht" || len(b.threads.confirmed) != 0 {
		t.Errorf("turns %v confirmed %v", b.turns, b.threads.confirmed)
	}
	b2 := newBench(t, store.ThreadProposed)
	b2.threads.confirmErr = errors.New("calendar refused")
	yes, _ := ParseTelegram(telegramTap("/mira th1 yes", 1001))
	b2.s.Handle(context.Background(), yes)
	if len(b2.told) != 1 || !strings.Contains(b2.told[0], "calendar refused") {
		t.Errorf("told %v", b2.told)
	}
	// Other Telegram events and other actions are not taps.
	if _, ok := ParseTelegram(map[string]any{"event_type": "telegram_sent"}); ok {
		t.Error("telegram_sent is a tap")
	}
	if _, ok := ParseTelegram(telegramTap("Vorschlag 2:mira:t1:2", 1001)); ok {
		t.Error("data without the command is a tap")
	}
	if _, ok := ParseApp(json.RawMessage(`{"action":"OPEN_x"}`), "u"); ok {
		t.Error("another app action is a tap")
	}
}

// PC-04 A tap on a proposal fills the field and confirms
func TestPC04_ATapOnAProposalFillsTheFieldAndConfirms(t *testing.T) {
	b := newBench(t, store.ThreadWaiting)
	th := b.threads.threads["th1"]
	th.Fields = map[string]string{"proposals": "Donnerstag passt.\nLieber Freitag.\nIch melde mich."}
	b.threads.threads["th1"] = th
	// When Anna taps the second proposal in the app
	raw, _ := json.Marshal(map[string]any{"action": "mira:th1:2"})
	tap, ok := ParseApp(raw, "u-anna")
	if !ok {
		t.Fatal("the tap does not parse")
	}
	b.s.Handle(context.Background(), tap)
	// Then the thread's field holds the second proposal and the thread is confirmed
	if strings.Join(b.threads.chosen, ",") != "th1: Lieber Freitag." || len(b.told) != 1 {
		t.Fatalf("chosen %v told %v", b.threads.chosen, b.told)
	}
	// A proposal that is not there does nothing.
	tg, _ := ParseTelegram(telegramTap("/mira th1 3", 1001))
	th.Fields = map[string]string{"proposals": "Nur einer."}
	b.threads.threads["th1"] = th
	b.s.Handle(context.Background(), tg)
	if len(b.threads.chosen) != 1 {
		t.Errorf("chosen %v", b.threads.chosen)
	}
}

func (b *bench) withLabels() {
	b.s.o.Labels = func(context.Context, store.Thread) Labels {
		return LabelsFor(i18n.DE).With(skills.ChoiceLabels{Choose: "Vorschlag {n} senden", Other: "Was anderes senden",
			Dismiss: "Ignorieren", Ask: "Was soll ich stattdessen antworten?"})
	}
}

func (b *bench) proposals(lines string) {
	th := b.threads.threads["th1"]
	th.Fields = map[string]string{"proposals": lines}
	b.threads.threads["th1"] = th
}

// PC-13 A tap on "Was anderes senden" asks what to send instead
func TestPC13_ATapOnOtherAsksWhatToSendInstead(t *testing.T) {
	b := newBench(t, store.ThreadWaiting)
	b.withLabels()
	b.proposals("Donnerstag passt.\nLieber Freitag.")
	tap, ok := ParseTelegram(telegramTap("/mira th1 other", 1001))
	if !ok {
		t.Fatal("the tap does not parse")
	}
	// When Anna taps "Was anderes senden" in Telegram
	b.s.Handle(context.Background(), tap)
	// Then the buttons go, the question goes into her chat and into the thread's log
	if got := strings.Join(b.services(), ","); got != "telegram_bot.answer_callback_query,telegram_bot.edit_replymarkup,telegram_bot.send_message" {
		t.Fatalf("calls %s", got)
	}
	if send := b.ha.calls[2].data; send["entity_id"] != "notify.telegram_anna" || send["message"] != "Was soll ich stattdessen antworten?" {
		t.Errorf("sent %+v", send)
	}
	if strings.Join(b.threads.notes, ",") != "th1: Was soll ich stattdessen antworten?" {
		t.Errorf("notes %v", b.threads.notes)
	}
	// And the thread stays open
	if len(b.threads.cancelled)+len(b.threads.confirmed)+len(b.threads.chosen) != 0 {
		t.Errorf("the thread was ended: %+v", b.threads)
	}
	// In the app the typed text is the turn, after the question in the log.
	b2 := newBench(t, store.ThreadWaiting)
	b2.withLabels()
	raw, _ := json.Marshal(map[string]any{"action": "mira:th1:other", "reply_text": "lieber Montag"})
	app, ok := ParseApp(raw, "u-anna")
	if !ok {
		t.Fatal("the app's other does not parse")
	}
	b2.s.Handle(context.Background(), app)
	if len(b2.threads.notes) != 1 || len(b2.turns) != 1 || b2.turns[0] != "anna/u-anna: lieber Montag" {
		t.Errorf("notes %v turns %v", b2.threads.notes, b2.turns)
	}
}

// PC-16 "Ignorieren" ends a thread that waits for a choice
func TestPC16_DismissEndsAThreadThatWaitsForAChoice(t *testing.T) {
	b := newBench(t, store.ThreadWaiting)
	b.proposals("Donnerstag passt.\nLieber Freitag.")
	tap, _ := ParseTelegram(telegramTap("/mira th1 no", 1001))
	b.s.Handle(context.Background(), tap)
	if strings.Join(b.threads.cancelled, ",") != "th1" {
		t.Errorf("cancelled %v", b.threads.cancelled)
	}
	// A yes still needs a thread that waits for it.
	b2 := newBench(t, store.ThreadWaiting)
	yes, _ := ParseTelegram(telegramTap("/mira th1 yes", 1001))
	b2.s.Handle(context.Background(), yes)
	if len(b2.threads.confirmed) != 0 {
		t.Errorf("confirmed %v", b2.threads.confirmed)
	}
}

// PC-18 A button of replaced proposals does nothing
func TestPC18_AButtonOfReplacedProposalsDoesNothing(t *testing.T) {
	b := newBench(t, store.ThreadWaiting)
	old := Version([]string{"Donnerstag passt.", "Lieber Freitag."})
	// Given Anna got new proposals after the old ones
	b.proposals("Montag passt.\nDienstag passt.")
	// When she taps the second proposal on the older message
	tap, ok := ParseTelegram(telegramTap("/mira th1 2."+old, 1001))
	if !ok || tap.Choice != "2" || tap.Version != old {
		t.Fatalf("tap %+v", tap)
	}
	b.s.Handle(context.Background(), tap)
	// Then nothing is chosen, Telegram's answer says it is done already, and a log line names it
	if len(b.threads.chosen) != 0 || b.ha.calls[0].data["message"] != "Das ist schon erledigt." {
		t.Fatalf("chosen %v calls %+v", b.threads.chosen, b.ha.calls)
	}
	if !strings.Contains(b.logs.String(), "outdated=true") {
		t.Errorf("log:\n%s", b.logs.String())
	}
	// The current mark chooses.
	now, _ := ParseTelegram(telegramTap("/mira th1 2."+Version([]string{"Montag passt.", "Dienstag passt."}), 1001))
	b.s.Handle(context.Background(), now)
	if strings.Join(b.threads.chosen, ",") != "th1: Dienstag passt." {
		t.Errorf("chosen %v", b.threads.chosen)
	}
}
