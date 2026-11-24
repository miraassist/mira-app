// Package choices puts buttons on a private question and takes a tap as its
// answer (docs/spec/private-choices.md): Telegram's inline keyboard through
// Home Assistant's telegram_bot, the companion app's actionable
// notifications. A tap on "Ja" confirms the thread, on "Nein" ends it, both
// without a model; a typed answer in the app is a turn of the person.
package choices

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/miraassist/mira-app/internal/config"
	haws "github.com/miraassist/mira-app/internal/ha/ws"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
)

// The choices a button carries (decision 5).
const (
	Yes   = "yes"
	No    = "no"
	Reply = "reply"
	// Other asks what to send instead (decision 17).
	Other = "other"
)

// telegramCommand starts a Telegram button's data: it must begin with "/"
// (decision 3, seen on the test HA 2026-11-24).
const telegramCommand = "/mira"

// appPrefix starts a companion app action's key.
const appPrefix = "mira:"

// Labels are the buttons' titles in the person's language; Proposal has {n}
// for the proposal's number. Other is empty when the target names no such
// button (decision 17); Ask is the question after it.
type Labels struct{ Yes, No, Reply, Proposal, Other, Dismiss, Ask string }

// LabelsFor are Mira's own titles.
func LabelsFor(lang i18n.Lang) Labels {
	if lang == i18n.EN {
		return Labels{Yes: "Yes", No: "No", Reply: "Reply", Proposal: "Proposal {n}", Dismiss: "No", Ask: "What should I send instead?"}
	}
	return Labels{Yes: "Ja", No: "Nein", Reply: "Antworten", Proposal: "Vorschlag {n}", Dismiss: "Nein", Ask: "Was soll ich stattdessen schicken?"}
}

// With puts a target's titles over Mira's own (decision 16).
func (l Labels) With(c skills.ChoiceLabels) Labels {
	set := func(dst *string, v string) {
		if strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
		}
	}
	set(&l.Yes, c.Yes)
	set(&l.No, c.No)
	set(&l.Proposal, c.Choose)
	set(&l.Other, c.Other)
	set(&l.Dismiss, c.Dismiss)
	set(&l.Ask, c.Ask)
	return l
}

// ForTarget are the titles of a target's buttons in lang: its own in that
// language, else its German ones, over Mira's.
func ForTarget(t *skills.Target, lang i18n.Lang) Labels {
	l := LabelsFor(lang)
	if t == nil {
		return l
	}
	if c, ok := t.Labels[string(lang)]; ok {
		return l.With(c)
	}
	return l.With(t.Labels[string(i18n.DE)])
}

// proposal is the title of proposal n.
func (l Labels) proposal(n int) string { return strings.ReplaceAll(l.Proposal, "{n}", strconv.Itoa(n)) }

// telegramButton is one button "Label:/mira <thread> <choice>"; a comma in
// the label would start the next button (decision 3).
func telegramButton(label, thread, choice string) string {
	return fmt.Sprintf("%s:%s %s %s", strings.ReplaceAll(label, ",", ""), telegramCommand, thread, choice)
}

// MaxProposals is how many proposals get a button (decision 2).
const MaxProposals = 3

// TelegramKeyboard is telegram_bot's inline_keyboard: for a yes/no question
// one row, for proposals a row of them and a row with Nein. No space after a
// comma: it would go into the next label (decision 3).
func TelegramKeyboard(thread string, l Labels, props []string) []string {
	proposals := len(props)
	if proposals == 0 {
		return []string{telegramButton(l.Yes, thread, Yes) + "," + telegramButton(l.No, thread, No)}
	}
	// One row per button: a label that says what it does is long
	// (decisions 16, 17).
	var rows []string
	for i := 1; i <= min(proposals, MaxProposals); i++ {
		rows = append(rows, telegramButton(l.proposal(i), thread, strconv.Itoa(i)+"."+Version(props)))
	}
	if l.Other != "" {
		rows = append(rows, telegramButton(l.Other, thread, Other))
	}
	return append(rows, telegramButton(l.Dismiss, thread, No))
}

// AppActions are the companion app's actions, with a typed answer as the
// last: Android shows three, so with proposals the first two and the typed
// answer, whose "nein" or "den dritten" the turn understands (decision 4).
func AppActions(thread string, l Labels, props []string) []map[string]any {
	proposals := len(props)
	reply := map[string]any{"action": appPrefix + thread + ":" + Reply, "title": l.Reply, "behavior": "textInput"}
	if proposals == 0 {
		return []map[string]any{
			{"action": appPrefix + thread + ":" + Yes, "title": l.Yes},
			{"action": appPrefix + thread + ":" + No, "title": l.No},
			reply,
		}
	}
	v := "." + Version(props)
	first := map[string]any{"action": appPrefix + thread + ":1" + v, "title": l.proposal(1)}
	if l.Other != "" {
		// The first proposal, "other" with a text field, "dismiss"
		// (decision 19); "2" or "3" typed there picks another.
		return []map[string]any{first,
			{"action": appPrefix + thread + ":" + Other, "title": l.Other, "behavior": "textInput"},
			{"action": appPrefix + thread + ":" + No, "title": l.Dismiss}}
	}
	out := []map[string]any{first}
	if proposals > 1 {
		out = append(out, map[string]any{"action": appPrefix + thread + ":2" + v, "title": l.proposal(2)})
	}
	return append(out, reply)
}

// ProposalsText lists the proposals under a question, numbered as their
// buttons.
func ProposalsText(proposals []string) string {
	var b strings.Builder
	for i, p := range proposals {
		if i == MaxProposals {
			break
		}
		fmt.Fprintf(&b, "\n%d. %s", i+1, p)
	}
	return b.String()
}

// Proposals are the lines of a thread's field proposals.
func Proposals(th store.Thread) []string {
	var out []string
	for _, line := range strings.Split(th.Fields["proposals"], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Version marks the proposals a button belongs to: a tap on a button of
// proposals that were replaced does nothing (docs/spec/private-choices.md,
// decision 22).
func Version(props []string) string {
	h := sha256.Sum256([]byte(strings.Join(props, "\n")))
	return hex.EncodeToString(h[:3])
}

// splitChoice is a choice "2.ab12cd" as its number and the proposals'
// version ("" for a button made before versions).
func splitChoice(c string) (choice, version string) {
	choice, version, _ = strings.Cut(c, ".")
	return choice, version
}

// isChoice is a proposal's number.
func isChoice(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= MaxProposals
}

// AppTag is the notification's tag for a thread's question.
func AppTag(thread string) string { return "mira-" + thread }

// Tap is one answer by a button.
type Tap struct {
	Thread, Choice string
	// Version is the proposals' a proposal's button belongs to.
	Version string
	// Telegram: the bot, the chat, the callback to answer and the message
	// whose buttons go.
	Bot, Chat  int64
	CallbackID string
	MessageID  int64
	// The app: the Home Assistant user who tapped, and a typed answer.
	HAUser string
	Reply  string
}

// Telegram reports whether it came from Telegram.
func (t Tap) Telegram() bool { return t.Chat != 0 }

// ParseTelegram reads a telegram_callback of the bot's event entity (its
// state's attributes); ok is false for any other event or another command.
func ParseTelegram(attrs map[string]any) (Tap, bool) {
	if attrs["event_type"] != "telegram_callback" {
		return Tap{}, false
	}
	data, _ := attrs["data"].(string)
	parts := strings.Fields(data)
	if len(parts) != 3 || parts[0] != telegramCommand {
		return Tap{}, false
	}
	if c, _ := splitChoice(parts[2]); c != Yes && c != No && c != Other && !isChoice(c) {
		return Tap{}, false
	}
	choice, version := splitChoice(parts[2])
	t := Tap{Thread: parts[1], Choice: choice, Version: version, Chat: toInt(attrs["chat_id"]), CallbackID: fmt.Sprint(attrs["id"])}
	if bot, ok := attrs["bot"].(map[string]any); ok {
		t.Bot = toInt(bot["id"])
	}
	if msg, ok := attrs["message"].(map[string]any); ok {
		t.MessageID = toInt(msg["message_id"])
	}
	if t.Chat == 0 || t.Thread == "" {
		return Tap{}, false
	}
	return t, true
}

// ParseApp reads a mobile_app_notification_action event; ok is false for
// an action that is not Mira's.
func ParseApp(data json.RawMessage, user string) (Tap, bool) {
	var d struct {
		Action    string `json:"action"`
		ReplyText string `json:"reply_text"`
	}
	if err := json.Unmarshal(data, &d); err != nil || !strings.HasPrefix(d.Action, appPrefix) {
		return Tap{}, false
	}
	parts := strings.Split(strings.TrimPrefix(d.Action, appPrefix), ":")
	if len(parts) != 2 || parts[0] == "" {
		return Tap{}, false
	}
	var version string
	parts[1], version = splitChoice(parts[1])
	switch {
	case parts[1] == Yes || parts[1] == No || isChoice(parts[1]):
	case parts[1] == Reply || parts[1] == Other:
		if strings.TrimSpace(d.ReplyText) == "" {
			return Tap{}, false
		}
	default:
		return Tap{}, false
	}
	return Tap{Thread: parts[0], Choice: parts[1], Version: version, HAUser: user, Reply: strings.TrimSpace(d.ReplyText)}, true
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// HA is the part of Home Assistant's client the taps use.
type HA interface {
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
	SubscribeTrigger(ctx context.Context, trigger map[string]any, handler haws.EventHandler) (func(), error)
	Entities(ctx context.Context) ([]haws.Entity, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
}

// Threads is the part of the threads a tap acts on.
type Threads interface {
	Get(ctx context.Context, id string) (store.Thread, error)
	Confirm(ctx context.Context, id string) (string, error)
	Cancel(ctx context.Context, id string) error
	// Choose puts a chosen proposal into the thread and says yes.
	Choose(ctx context.Context, id, proposal string) (string, error)
	// Note writes Mira's question after "other" into the thread's log, so
	// the person's next turn sees it (decision 17).
	Note(ctx context.Context, id, text string) error
}

// Options make the taps' service.
type Options struct {
	HA      HA
	Threads Threads
	// Persons are the persons with their channels and Home Assistant users.
	Persons func() map[string]config.Person
	// Tell says a thread's outcome to its person privately.
	Tell func(ctx context.Context, person, text string) error
	// Turn takes a typed answer as a turn of the person, made in the app by
	// that Home Assistant user.
	Turn func(ctx context.Context, person, haUser, text string) error
	Lang func() i18n.Lang
	// Labels are a thread's button titles for its person (decision 16).
	Labels func(ctx context.Context, th store.Thread) Labels
	Log    *slog.Logger
}

// Service hears the taps and acts on them.
type Service struct {
	o    Options
	taps chan Tap
	// chats maps a Telegram chat's notify entity by its unique id "<bot>_<chat>".
	mu    sync.Mutex
	chats map[string]string
}

// New makes the service; Start subscribes.
func New(o Options) *Service {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{o: o, taps: make(chan Tap, 64), chats: map[string]string{}}
}

// Start subscribes to the bots' event entities and the app's actions and
// works the taps until ctx ends (decision 11: the handlers only pass a tap
// on).
func (s *Service) Start(ctx context.Context) error {
	ents, err := s.o.HA.Entities(ctx)
	if err != nil {
		return fmt.Errorf("reading Home Assistant's entities for Telegram's taps: %w", err)
	}
	var events []string
	s.mu.Lock()
	for _, e := range ents {
		if e.Platform != "telegram_bot" {
			continue
		}
		switch {
		case strings.HasPrefix(e.EntityID, "event."):
			events = append(events, e.EntityID)
		case strings.HasPrefix(e.EntityID, "notify."):
			s.chats[string(e.UniqueID)] = e.EntityID
		}
	}
	s.mu.Unlock()
	if len(events) > 0 {
		if _, err := s.o.HA.SubscribeTrigger(ctx, map[string]any{"platform": "state", "entity_id": events}, s.onTelegram); err != nil {
			return fmt.Errorf("subscribing to Telegram's taps (%s): %w", strings.Join(events, ", "), err)
		}
	}
	if _, err := s.o.HA.Subscribe(ctx, "mobile_app_notification_action", s.onApp); err != nil {
		return fmt.Errorf("subscribing to the companion app's taps: %w", err)
	}
	s.o.Log.Info("private choices on", "telegram_bots", len(events))
	go s.work(ctx)
	return nil
}

func (s *Service) pass(t Tap) {
	select {
	case s.taps <- t:
	default:
		s.o.Log.Warn("a tap is dropped: too many waiting", "thread", t.Thread, "choice", t.Choice)
	}
}

func (s *Service) onTelegram(ev haws.Event) {
	var v haws.TriggerVariables
	if err := json.Unmarshal(ev.Variables, &v); err != nil || v.Trigger.ToState == nil {
		return
	}
	if t, ok := ParseTelegram(v.Trigger.ToState.Attributes); ok {
		s.pass(t)
	}
}

func (s *Service) onApp(ev haws.Event) {
	var raw struct {
		Context struct {
			UserID string `json:"user_id"`
		} `json:"context"`
	}
	if err := json.Unmarshal(ev.Raw, &raw); err != nil {
		// Without the user the tap cannot be checked (decision 6): it is
		// refused later as not the person's.
		s.o.Log.Warn("a companion app tap's context cannot be read", "error", err, "event", clipRaw(ev.Raw))
	}
	if t, ok := ParseApp(ev.Data, raw.Context.UserID); ok {
		s.pass(t)
	}
}

func clipRaw(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

func (s *Service) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-s.taps:
			s.Handle(ctx, t)
		}
	}
}

// texts are what a tap is answered with.
type texts struct{ done, declined, notYours, already, failed string }

func (s *Service) texts() texts {
	lang := i18n.DE
	if s.o.Lang != nil {
		lang = s.o.Lang()
	}
	if lang == i18n.EN {
		return texts{done: "Done.", declined: "Declined.", notYours: "This is not your question.", already: "That is done already.",
			failed: "That did not work: %s"}
	}
	return texts{done: "Erledigt.", declined: "Abgelehnt.", notYours: "Das ist nicht deine Frage.", already: "Das ist schon erledigt.",
		failed: "Das hat nicht geklappt: %s"}
}

// errNotYours: the tap comes from someone the thread does not belong to.
var errNotYours = errors.New("not the person asked")

// Handle acts on one tap (decisions 6, 7, 9, 10, 12).
func (s *Service) Handle(ctx context.Context, t Tap) {
	tx := s.texts()
	th, err := s.o.Threads.Get(ctx, t.Thread)
	if err != nil {
		s.o.Log.Info("a tap for a thread that is gone", "thread", t.Thread, "choice", t.Choice, "error", err)
		s.answer(ctx, t, tx.already)
		return
	}
	if err := s.mine(ctx, t, th); err != nil {
		s.o.Log.Warn("a tap refused", "thread", t.Thread, "person", th.Person, "chat", t.Chat, "ha_user", t.HAUser, "error", err)
		s.answer(ctx, t, tx.notYours)
		return
	}
	if t.Choice == Other {
		s.other(ctx, t, th)
		return
	}
	if t.Choice == Reply {
		// A typed answer is a turn of the person (decision 12).
		if err := s.o.Turn(ctx, th.Person, t.HAUser, t.Reply); err != nil {
			s.o.Log.Warn("a typed answer could not be a turn", "thread", t.Thread, "person", th.Person, "error", err)
		}
		return
	}
	if isChoice(t.Choice) {
		s.choose(ctx, t, th)
		return
	}
	// "Nein" or "dismiss" ends a thread also while it waits for a choice
	// (decision 21); a yes only one that waits for it.
	if !store.ThreadOpen(th.State) || (t.Choice == Yes && th.State != store.ThreadProposed) {
		s.o.Log.Info("a tap on a thread that waits for no yes", "thread", t.Thread, "state", th.State, "choice", t.Choice)
		s.answer(ctx, t, tx.already)
		return
	}
	if t.Choice == No {
		s.answer(ctx, t, tx.declined)
		if err := s.o.Threads.Cancel(ctx, t.Thread); err != nil {
			s.o.Log.Warn("a tap's no could not end the thread", "thread", t.Thread, "error", err)
		}
		s.o.Log.Info("a thread declined by a tap", "thread", t.Thread, "person", th.Person)
		return
	}
	// Answered at once: Telegram refuses an answer minutes later (decision 9).
	s.answer(ctx, t, tx.done)
	done, err := s.o.Threads.Confirm(ctx, t.Thread)
	if err != nil {
		s.o.Log.Warn("a tap's yes failed", "thread", t.Thread, "person", th.Person, "error", err)
		done = fmt.Sprintf(tx.failed, err)
	} else {
		s.o.Log.Info("a thread confirmed by a tap", "thread", t.Thread, "person", th.Person)
	}
	if s.o.Tell != nil && strings.TrimSpace(done) != "" {
		if err := s.o.Tell(ctx, th.Person, done); err != nil {
			s.o.Log.Warn("a tap's outcome could not be told", "thread", t.Thread, "person", th.Person, "error", err)
		}
	}
}

// choose takes a tap on a proposal: its line goes into the thread's chosen
// field, and the thread is confirmed (decision 7).
func (s *Service) choose(ctx context.Context, t Tap, th store.Thread) {
	tx := s.texts()
	n, _ := strconv.Atoi(t.Choice)
	props := Proposals(th)
	if !store.ThreadOpen(th.State) || n > len(props) || (t.Version != "" && t.Version != Version(props)) {
		s.o.Log.Info("a tap on a proposal that is not there", "thread", t.Thread, "state", th.State, "choice", t.Choice,
			"proposals", len(props), "outdated", t.Version != "" && t.Version != Version(props))
		s.answer(ctx, t, tx.already)
		return
	}
	s.answer(ctx, t, tx.done)
	done, err := s.o.Threads.Choose(ctx, t.Thread, props[n-1])
	if err != nil {
		s.o.Log.Warn("a tap's proposal failed", "thread", t.Thread, "person", th.Person, "choice", t.Choice, "error", err)
		done = fmt.Sprintf(tx.failed, err)
	} else {
		s.o.Log.Info("a thread's proposal chosen by a tap", "thread", t.Thread, "person", th.Person, "choice", t.Choice)
	}
	if s.o.Tell != nil && strings.TrimSpace(done) != "" {
		if err := s.o.Tell(ctx, th.Person, done); err != nil {
			s.o.Log.Warn("a tap's outcome could not be told", "thread", t.Thread, "person", th.Person, "error", err)
		}
	}
}

// other takes a tap on "other" (decision 17): Mira's question goes into the
// thread's log; in Telegram also into the chat, and the person's next
// message there is the turn; in the app the typed text is that turn.
func (s *Service) other(ctx context.Context, t Tap, th store.Thread) {
	tx := s.texts()
	if !store.ThreadOpen(th.State) {
		s.answer(ctx, t, tx.already)
		return
	}
	labels := LabelsFor(i18n.DE)
	if s.o.Labels != nil {
		labels = s.o.Labels(ctx, th)
	}
	ask := labels.Ask
	if err := s.o.Threads.Note(ctx, th.ID, ask); err != nil {
		s.o.Log.Warn("could not note the question after other in the thread", "thread", th.ID, "error", err)
	}
	s.o.Log.Info("a thread's person wants to send something else", "thread", th.ID, "person", th.Person, "telegram", t.Telegram())
	if !t.Telegram() {
		if err := s.o.Turn(ctx, th.Person, t.HAUser, t.Reply); err != nil {
			s.o.Log.Warn("a typed answer could not be a turn", "thread", t.Thread, "person", th.Person, "error", err)
		}
		return
	}
	s.answer(ctx, t, ask)
	chat := s.chatEntity(ctx, t.Bot, t.Chat)
	if chat == "" {
		s.o.Log.Warn("could not ask what to send instead: the chat's notify entity is unknown", "thread", th.ID, "chat", t.Chat)
		return
	}
	if err := s.o.HA.CallService(ctx, "telegram_bot", "send_message", map[string]any{"entity_id": chat, "message": ask}, nil); err != nil {
		s.o.Log.Warn("could not ask what to send instead", "thread", th.ID, "chat", chat, "error", err)
	}
}

// mine checks that the tap comes from the person the thread belongs to
// (decision 6): their Telegram chat, or their Home Assistant user.
func (s *Service) mine(ctx context.Context, t Tap, th store.Thread) error {
	p, ok := s.o.Persons()[th.Person]
	if th.Person == "" || !ok {
		return fmt.Errorf("the thread belongs to no known person (%q)", th.Person)
	}
	if !t.Telegram() {
		if t.HAUser == "" || t.HAUser != p.UserID {
			return errNotYours
		}
		return nil
	}
	chat := s.chatEntity(ctx, t.Bot, t.Chat)
	for _, ch := range p.Channels {
		if ch == chat && chat != "" {
			return nil
		}
	}
	return errNotYours
}

// chatEntity is the notify entity of a bot's chat, read again from Home
// Assistant when it is new.
func (s *Service) chatEntity(ctx context.Context, bot, chat int64) string {
	key := fmt.Sprintf("%d_%d", bot, chat)
	s.mu.Lock()
	id, ok := s.chats[key]
	s.mu.Unlock()
	if ok {
		return id
	}
	ents, err := s.o.HA.Entities(ctx)
	if err != nil {
		s.o.Log.Warn("could not read the entities for a Telegram chat", "chat", chat, "error", err)
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range ents {
		if e.Platform == "telegram_bot" && strings.HasPrefix(e.EntityID, "notify.") {
			s.chats[string(e.UniqueID)] = e.EntityID
		}
	}
	return s.chats[key]
}

// answer answers a Telegram tap and removes the buttons, so a second tap
// cannot come (decision 9); an app's tap needs no answer.
func (s *Service) answer(ctx context.Context, t Tap, text string) {
	if !t.Telegram() {
		return
	}
	if err := s.o.HA.CallService(ctx, "telegram_bot", "answer_callback_query",
		map[string]any{"callback_query_id": t.CallbackID, "message": text}, nil); err != nil {
		s.o.Log.Warn("could not answer a Telegram tap", "thread", t.Thread, "error", err)
	}
	if t.MessageID == 0 {
		return
	}
	if err := s.o.HA.CallService(ctx, "telegram_bot", "edit_replymarkup",
		map[string]any{"message_id": t.MessageID, "chat_id": t.Chat, "inline_keyboard": []string{}}, nil); err != nil {
		s.o.Log.Warn("could not remove a Telegram question's buttons", "thread", t.Thread, "error", err)
	}
}
