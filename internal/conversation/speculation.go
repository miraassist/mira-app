package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira-app/internal/agent"
	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/excerpt"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/llm"
	"github.com/miraassist/mira-app/internal/peek"
	"github.com/miraassist/mira-app/internal/prompt"
	"github.com/miraassist/mira-app/internal/sidechannel"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/tools"
)

// Speculative turns (docs/spec/speculative-turns.md): on a reading of the
// side channel Mira builds the turn Home Assistant will most likely send and
// runs its rounds early, only reading. A turn whose request for a round is
// the same takes the run's round instead of asking the model.

const (
	// speculateFrom is the side channel's guess that the user is done from
	// which a reading starts a run: Smart Turn's own threshold.
	speculateFrom = 0.5
	// keepAfterFinal is how long a run waits for its turn after the side
	// channel's final transcript.
	keepAfterFinal = 10 * time.Second
	// keepAtMost ends a run whose side-channel turn never ended (the side
	// channel lost it): a safety net, no signal comes.
	keepAtMost = 30 * time.Second
)

// specRound is one round of a run: its request's hash, then the model's
// answer.
type specRound struct {
	hash string
	done chan struct{}
	out  *llm.ChatResponse
	err  error
}

// specRun is one speculative run.
type specRun struct {
	side         string
	rev          int
	device, text string
	started      time.Time
	cancel       context.CancelFunc
	timer        interface{ Stop() bool }

	mu     sync.Mutex
	rounds []*specRound
	// changed is closed and replaced whenever a round is added or the run
	// is over.
	changed chan struct{}
	// over: no further round comes; why says how its rounds ended. ended
	// is why the run was ended, for its log line.
	over  bool
	why   string
	ended string
	// taken: a turn uses the run; only it ends it.
	taken bool
	// logged: its log line is written.
	logged bool
}

// speculation is the engine's runs, by side-channel turn, and what it
// remembers per satellite to build a run's turn.
type speculation struct {
	mu   sync.Mutex
	runs map[string]*specRun
	// final is the final text of the side-channel turns that sent one.
	final map[string]string
	// voice, lang and conv are per device: the person the voice named in its
	// last turn, the language Home Assistant sent with it, and the
	// conversation its answer left listening.
	voice, lang, conv map[string]string
}

func (e *Engine) spec() *speculation {
	e.specOnce.Do(func() {
		e.specs = &speculation{runs: map[string]*specRun{}, final: map[string]string{},
			voice: map[string]string{}, lang: map[string]string{}, conv: map[string]string{}}
	})
	return e.specs
}

// noteForSpeculation remembers of a satellite's turn what the next run
// there builds on (decision 1).
func (e *Engine) noteForSpeculation(req Request, person, by string, listening bool) {
	if req.Channel != config.ChannelVoice || req.DeviceID == "" || req.Speculative {
		return
	}
	s := e.spec()
	s.mu.Lock()
	defer s.mu.Unlock()
	if by == prompt.ByVoice {
		s.voice[req.DeviceID] = person
	}
	s.lang[req.DeviceID] = req.Language
	if listening {
		s.conv[req.DeviceID] = req.ConversationID
	} else {
		delete(s.conv, req.DeviceID)
	}
}

// Speculate takes an event of the side channel and the satellite its turn
// belongs to ("" for none). It runs on the side channel's read loop: it
// starts and ends runs, never waits.
func (e *Engine) Speculate(ev sidechannel.Event, device string) {
	if device == "" {
		return
	}
	s := e.spec()
	switch ev.Kind {
	case sidechannel.Reading:
		if ev.Complete == nil || *ev.Complete < speculateFrom {
			return
		}
		e.startRun(ev.Turn, ev.Rev, device, ev.Text)
	case sidechannel.ReadingStale:
		s.mu.Lock()
		r := s.runs[ev.Turn]
		s.mu.Unlock()
		if r != nil && r.rev == ev.Rev {
			e.endRun(r, "stale")
		}
	case sidechannel.TurnFinal:
		s.mu.Lock()
		r := s.runs[ev.Turn]
		s.final[ev.Turn] = ev.Text
		s.mu.Unlock()
		if r == nil || r.text != ev.Text {
			r = e.startRun(ev.Turn, ev.Rev, device, ev.Text)
		}
		if r != nil {
			e.expire(r, keepAfterFinal)
		}
	}
}

// startRun starts a run, ending the side-channel turn's earlier one.
func (e *Engine) startRun(side string, rev int, device, text string) *specRun {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	s := e.spec()
	// A run lives at most keepAtMost; it is not one of the turns the
	// service waits for on its stop.
	ctx, cancel := context.WithCancel(context.Background())
	r := &specRun{side: side, rev: rev, device: device, text: text, started: e.clock(), cancel: cancel,
		changed: make(chan struct{})}
	s.mu.Lock()
	old := s.runs[side]
	if old != nil && old.isTaken() {
		s.mu.Unlock()
		cancel()
		return nil
	}
	s.runs[side] = r
	s.mu.Unlock()
	if old != nil {
		e.endRun(old, "replaced")
	}
	e.expire(r, keepAtMost)
	go e.run(ctx, r)
	return r
}

// expire ends a run after d unless a turn took it.
func (e *Engine) expire(r *specRun, d time.Duration) {
	t := clock.Or(e.timers).AfterFunc(d, func() {
		if !r.isTaken() {
			e.endRun(r, "expired")
		}
	})
	r.mu.Lock()
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = t
	r.mu.Unlock()
}

// endRun ends a run and forgets it; the first reason counts.
func (e *Engine) endRun(r *specRun, why string) {
	r.mu.Lock()
	if r.ended == "" {
		r.ended = why
	}
	r.mu.Unlock()
	r.finish(why)
	r.cancel()
	r.mu.Lock()
	if r.timer != nil {
		r.timer.Stop()
	}
	r.mu.Unlock()
	s := e.spec()
	s.mu.Lock()
	if s.runs[r.side] == r {
		delete(s.runs, r.side)
		delete(s.final, r.side)
	}
	s.mu.Unlock()
	e.logRun(r, 0, 0, -1)
}

func (r *specRun) isTaken() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.taken
}

// finish marks the run over; the first reason counts.
func (r *specRun) finish(why string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.over {
		return
	}
	r.over, r.why = true, why
	close(r.changed)
	r.changed = make(chan struct{})
}

// add starts a round with its request's hash.
func (r *specRun) add(hash string) *specRound {
	sr := &specRound{hash: hash, done: make(chan struct{})}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rounds = append(r.rounds, sr)
	close(r.changed)
	r.changed = make(chan struct{})
	return sr
}

// round waits for the run's round n: nil when the run is over without it.
func (r *specRun) round(ctx context.Context, n int) *specRound {
	for {
		r.mu.Lock()
		if n < len(r.rounds) {
			sr := r.rounds[n]
			r.mu.Unlock()
			return sr
		}
		if r.over {
			r.mu.Unlock()
			return nil
		}
		ch := r.changed
		r.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil
		}
	}
}

// requestHash is what decides whether a turn's round is a run's: the whole
// request as it goes to the model (decision 2).
func requestHash(req llm.ChatRequest) string {
	b, err := json.Marshal(req)
	if err != nil {
		return "" // never the same as another
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// run builds the turn Home Assistant will most likely send and runs its
// rounds while they only read (docs/spec/speculative-turns.md, 2).
func (e *Engine) run(ctx context.Context, r *specRun) {
	err := e.runRounds(ctx, r)
	var no errNoRun
	switch {
	case err == nil:
		r.finish("done")
	case ctx.Err() != nil:
		r.finish("ended")
	case errors.As(err, &no):
		e.log.Debug("no speculative run", "device", r.device, "text", excerpt.Of(r.text), "reason", no.reason)
		e.endRun(r, "skipped: "+no.reason)
	default:
		e.log.Info("speculative run failed", "device", r.device, "text", excerpt.Of(r.text), "error", err)
		e.endRun(r, "failed")
	}
}

func (e *Engine) runRounds(ctx context.Context, r *specRun) error {
	s := e.spec()
	s.mu.Lock()
	voice, lang, conv := s.voice[r.device], s.lang[r.device], s.conv[r.device]
	s.mu.Unlock()
	if conv == "" {
		conv = ulid.Make().String()
	}
	req := Request{Channel: config.ChannelVoice, ConversationID: conv, DeviceID: r.device, Language: lang, Text: r.text,
		Speculative: true, Voice: voice}
	ctx, note := peek.With(ctx)
	person, personBy := "", ""
	if e.identify != nil {
		person, personBy = e.identify(ctx, req)
	}
	person, personBy = e.continued(req, person, personBy)
	snap := e.structure()
	area := snap.DeviceArea[req.DeviceID]
	own, place := belongsTo(req.Channel, req.ConversationID, person, area, snap.Private[area])
	var pre turnPre
	pre.messageText, pre.followedUp, pre.announced = e.messageContext(ctx, req.DeviceID)
	pre.thread = e.threadOf(ctx, req, pre.followedUp, pre.announced, area, snap)
	if pre.thread != "" {
		return errNoRun{"the turn runs in a thread"}
	}
	pre.own = own
	key := own
	ts := &tools.TurnState{SessionKey: key, Channel: req.Channel, UserText: req.Text, Person: person, Area: area,
		HoldsSummaries: true, DeviceID: req.DeviceID, Place: place, PersonBy: personBy, PersonName: e.nameOf(person)}
	ctx, _ = e.agents.Enter(ctx, agent.Spec{Name: config.ChannelAgent(req.Channel), Profile: agent.Conversation,
		Rights: agent.AllRights}, agent.CallContext{Origin: originOf(req), Person: person, Place: area, SessionKey: key,
		DeviceID: req.DeviceID, Rights: agent.AllRights}, ts)
	b, _, err := e.build(ctx, ts, req, snap, pre, e.clock(), true)
	if err != nil {
		return err
	}
	if left := note.Left(); len(left) > 0 {
		return errNoRun{"it would " + left[0]}
	}
	ctx = b.ctx
	msgs := b.built.Messages
	for round := 0; ; round++ {
		chatReq := e.chatRequestOn(b.modelName, req.Channel, msgs, b.built)
		chatReq.Tools = append(chatReq.Tools, e.skillTools(ctx, key)...)
		if round >= b.model.MaxToolRounds {
			chatReq.ToolChoice = "none"
		}
		sr := r.add(requestHash(chatReq))
		release, ok := e.agents.TryAcquire(b.modelName, b.model, func() { e.endRun(r, "yielded") })
		if !ok {
			sr.err = errors.New("no free place at the model")
			close(sr.done)
			return errNoRun{"no free place at the model"}
		}
		out, err := b.client.Chat(ctx, chatReq)
		release()
		sr.out, sr.err = out, err
		close(sr.done)
		if err != nil {
			return err
		}
		msg := out.Message()
		msg.Role = llm.RoleAssistant
		msg.Content = stripThinking(msg.Content)
		if len(msg.ToolCalls) == 0 || round >= b.model.MaxToolRounds {
			return nil
		}
		ensureCallIDs(msg.ToolCalls, round)
		// The first call that does not only read ends the run after this
		// round (decision 3, 4).
		for _, call := range msg.ToolCalls {
			if spec, ok := e.tools.Lookup(call.Function.Name); !ok || !spec.ReadOnly {
				return nil
			}
		}
		msgs = append(msgs, msg)
		for _, call := range msg.ToolCalls {
			result := e.runTool(ctx, ts, call, b.model)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Function.Name, Content: result})
		}
		// As a streamed voice turn does: the round's text was spoken.
		if said := strings.TrimSpace(msg.Content); said != "" {
			msgs[len(msgs)-1].Content += fmt.Sprintf(e.texts(ctx).alreadySaid, said)
		}
	}
}

// adoption is a turn's use of a run.
type adoption struct {
	run    *specRun
	taken  int
	waited time.Duration
	// differed is the round whose request was not the run's, -1 for none.
	differed int
}

// takeRound is the run's answer for the turn's round n with this request,
// if a run of its satellite has the same request (docs/spec/speculative-
// turns.md, 3). At round 0 the turn waits until each run of its satellite
// has built its first request.
func (e *Engine) takeRound(ctx context.Context, a *adoption, req Request, n int, chatReq llm.ChatRequest) (*llm.ChatResponse, bool) {
	if req.Channel != config.ChannelVoice || req.DeviceID == "" || req.Speculative {
		return nil, false
	}
	if n > 0 && a.run == nil {
		return nil, false
	}
	hash := ""
	began := e.clock()
	if n == 0 {
		s := e.spec()
		s.mu.Lock()
		var runs []*specRun
		for _, r := range s.runs {
			if r.device == req.DeviceID {
				runs = append(runs, r)
			}
		}
		s.mu.Unlock()
		if len(runs) == 0 {
			return nil, false
		}
		hash = requestHash(chatReq)
		for _, r := range runs {
			sr := r.round(ctx, 0)
			if sr != nil && sr.hash == hash && a.run == nil {
				r.mu.Lock()
				r.taken = true
				r.mu.Unlock()
				a.run = r
				continue
			}
			e.endRun(r, "no_match")
		}
		if a.run == nil {
			return nil, false
		}
	}
	r := a.run
	sr := r.round(ctx, n)
	if sr == nil {
		return nil, false
	}
	if hash == "" {
		hash = requestHash(chatReq)
	}
	if sr.hash != hash {
		a.differed = n
		return nil, false
	}
	select {
	case <-sr.done:
	case <-ctx.Done():
		return nil, false
	}
	if sr.err != nil {
		return nil, false
	}
	a.taken++
	a.waited += e.clock().Sub(began)
	return sr.out, true
}

// doneWith ends a turn's run: its log line and its place.
func (e *Engine) doneWith(a *adoption) {
	if a.run == nil {
		return
	}
	r := a.run
	r.finish("taken")
	r.cancel()
	s := e.spec()
	s.mu.Lock()
	if s.runs[r.side] == r {
		delete(s.runs, r.side)
		delete(s.final, r.side)
	}
	s.mu.Unlock()
	e.logRun(r, a.taken, a.waited, a.differed)
}

// logRun writes the run's one line (docs/spec/speculative-turns.md, 3).
func (e *Engine) logRun(r *specRun, taken int, waited time.Duration, differed int) {
	r.mu.Lock()
	if r.logged {
		r.mu.Unlock()
		return
	}
	r.logged = true
	why, rounds, how := r.ended, len(r.rounds), r.why
	if r.taken {
		why = "taken"
	}
	r.mu.Unlock()
	attrs := []any{"side_turn", r.side, "rev", r.rev, "device", r.device, "text", excerpt.Of(r.text), "ended", why,
		"rounds", rounds, "rounds_ended", how, "ms", e.clock().Sub(r.started).Milliseconds()}
	if why == "taken" {
		attrs = append(attrs, "rounds_taken", taken, "waited_ms", waited.Milliseconds())
		if differed >= 0 {
			attrs = append(attrs, "differed_at_round", differed)
		}
	}
	e.log.Info("speculation", attrs...)
}

// peekSession is a run's session and history: read, not opened (an expired
// one is empty), and no run where the turn would compact (decision 5).
func (e *Engine) peekSession(ctx context.Context, key string, req Request, area string) (store.Session, []llm.Message, error) {
	sess := store.Session{Key: key, Channel: string(req.Channel), ConversationID: req.ConversationID,
		DeviceID: req.DeviceID, AreaID: area, Language: req.Language}
	got, err := e.store.GetSession(ctx, key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return sess, nil, nil
	case err != nil:
		return store.Session{}, nil, err
	case got.ExpiresAt.Before(e.clock()):
		return sess, nil, nil
	}
	if got.ThreadID != "" {
		return store.Session{}, nil, errNoRun{"the conversation is linked to a thread"}
	}
	sess.ThreadID = got.ThreadID
	history, err := e.history(ctx, key)
	if err != nil {
		return store.Session{}, nil, err
	}
	stored, err := e.store.ChatMessages(ctx, key)
	if err != nil {
		return store.Session{}, nil, err
	}
	if turnStarts(stored) >= e.cfg.Sessions.MaxTurns {
		return store.Session{}, nil, errNoRun{"the history is due for compaction"}
	}
	return sess, history, nil
}

// peekLang is the turn's language as languages.Turn finds it, without
// remembering the language sent.
func (e *Engine) peekLang(ctx context.Context, sent, person string) i18n.Lang {
	if l, err := i18n.Parse(sent); sent != "" && err == nil {
		return l
	}
	// Without a language sent, Turn remembers nothing.
	return e.languages.Turn(ctx, "", "", person)
}
