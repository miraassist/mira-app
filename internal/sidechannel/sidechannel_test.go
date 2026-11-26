package sidechannel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/retry"
	"github.com/miraassist/mira-app/internal/testsupport/logcap"
)

// got collects what a client hands on.
type got struct {
	mu     sync.Mutex
	events []Event
	device []string
	more   chan struct{}
}

func newGot() *got { return &got{more: make(chan struct{}, 100)} }

func (g *got) handle(ev Event, device string) {
	g.mu.Lock()
	g.events = append(g.events, ev)
	g.device = append(g.device, device)
	g.mu.Unlock()
	g.more <- struct{}{}
}

func (g *got) await(t *testing.T, n int) {
	t.Helper()
	for {
		g.mu.Lock()
		have := len(g.events)
		g.mu.Unlock()
		if have >= n {
			return
		}
		<-g.more // go test -timeout names a broken client (docs/spec/tests.md, rule 1)
	}
}

// stream serves the recorded stream once per connection, then ends it.
func stream(t *testing.T, file string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
}

func TestSC01_WithoutURLThereIsNoClient(t *testing.T) {
	if c := New(Options{URL: "  "}); c != nil {
		t.Fatal("a client without a URL")
	}
}

func TestSC02_TheEventsOfATurnAreReadInOrder(t *testing.T) {
	srv := stream(t, "testdata/stammered.sse")
	defer srv.Close()
	log, _ := logcap.Logger()
	g := newGot()
	clk := clock.NewFake(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	c := New(Options{URL: srv.URL, Clock: clk, Logger: log, Handle: g.handle,
		Listening: func() []string { return []string{"kitchen"} }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	g.await(t, 9)

	g.mu.Lock()
	defer g.mu.Unlock()
	kinds := []Kind{TurnStarted, SpeechStarted, SpeechPaused, Reading, SpeechStarted, ReadingStale, SpeechPaused, Reading, TurnFinal}
	for i, k := range kinds {
		if g.events[i].Kind != k {
			t.Fatalf("event %d is %s, want %s", i, g.events[i].Kind, k)
		}
		if g.events[i].Turn != "06deba79e74c" || g.device[i] != "kitchen" {
			t.Fatalf("event %d: turn %q at %q", i, g.events[i].Turn, g.device[i])
		}
	}
	r1, stale, r2, final := g.events[3], g.events[5], g.events[7], g.events[8]
	if r1.Rev != 1 || r1.Text != "Schalte das Licht im." || r1.Complete == nil || *r1.Complete != 0.177 {
		t.Fatalf("first reading: %+v", r1)
	}
	if stale.Rev != 1 {
		t.Fatalf("stale: %+v", stale)
	}
	if r2.Rev != 2 || r2.AudioS != 3.968 {
		t.Fatalf("second reading: %+v", r2)
	}
	if final.Text != "Schalte das Licht im Wohnzimmer an." || final.Source != "speculative" || final.Rev != 2 {
		t.Fatalf("final: %+v", final)
	}
	if final.Sent.Unix() != 1795621255 {
		t.Fatalf("sent %v", final.Sent)
	}
}

func TestSC03_ALostConnectionIsTakenUpAgain(t *testing.T) {
	var mu sync.Mutex
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: turn.started\ndata: {\"turn\": \"a\", \"t\": 1}\n\n"))
	}))
	defer srv.Close()
	log, lines := logcap.Logger()
	clk := clock.NewFake(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	g := newGot()
	c := New(Options{URL: srv.URL, Clock: clk, Logger: log, Handle: g.handle})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	// Each connection hands one event and ends; the wait after each is 1 s:
	// a connection that worked starts the count again.
	for i := 1; i <= 3; i++ {
		g.await(t, i)
		<-clk.AwaitWaiters(1)
		if d := clk.AdvanceToNext(); d != time.Second {
			t.Fatalf("wait %d is %v, want 1s", i, d)
		}
	}
	g.await(t, 4)
	if n := len(lines.String()); n == 0 {
		t.Fatal("no warning")
	}
	if l := lines.Line("side channel lost", "stream ended"); l == "" {
		t.Fatalf("no warning naming the end:\n%s", lines)
	}

	// The side channel down: the waits grow 1 s, 2 s, 4 s, one warning.
	srv.Close()
	<-clk.AwaitWaiters(1)
	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		<-clk.AwaitWaiters(1)
		if d := clk.AdvanceToNext(); d != want {
			t.Fatalf("wait %v, want %v", d, want)
		}
	}
}

func TestSC03_TheWaitIsBoundedAtAMinute(t *testing.T) {
	if d := Policy.Wait(20); d != time.Minute {
		t.Fatalf("wait after 20 failures %v", d)
	}
}

func TestSC04_AURLThatIsNoSideChannelEndsTheSubscription(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"404": func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"json": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"turn": null}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			log, lines := logcap.Logger()
			c := New(Options{URL: srv.URL, Logger: log, Clock: clock.NewFake(time.Unix(0, 0))})
			err := c.Run(context.Background())
			if err == nil || retry.Retryable(err) {
				t.Fatalf("error %v, want a fatal one", err)
			}
			if lines.Line("level=ERROR", "side channel refused", srv.URL) == "" {
				t.Fatalf("no error naming the URL:\n%s", lines)
			}
		})
	}
}

func TestSC04_AnEventThatDoesNotParseIsLeftOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: reading\ndata: {broken\n\nevent: turn.final\ndata: {\"turn\": \"b\", \"text\": \"Licht aus.\"}\n\n"))
	}))
	defer srv.Close()
	log, lines := logcap.Logger()
	g := newGot()
	c := New(Options{URL: srv.URL, Logger: log, Handle: g.handle, Clock: clock.NewFake(time.Unix(0, 0))})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	g.await(t, 1)
	if g.events[0].Kind != TurnFinal || g.events[0].Text != "Licht aus." {
		t.Fatalf("%+v", g.events[0])
	}
	if lines.Line("level=WARN", "not understood", "{broken") == "" {
		t.Fatalf("no warning with the event's start:\n%s", lines)
	}
}

func TestSC05_ATurnBelongsToTheOneListeningSatellite(t *testing.T) {
	c := New(Options{URL: "http://side"})
	for _, tc := range []struct {
		listening []string
		want      string
	}{
		{[]string{"kitchen"}, "kitchen"},
		{[]string{"kitchen", "bath"}, ""},
		{nil, ""},
	} {
		var device string
		c.opts.Listening = func() []string { return tc.listening }
		c.opts.Handle = func(ev Event, d string) { device = d }
		c.dispatch(string(TurnStarted), `{"turn": "t1"}`)
		c.dispatch(string(Reading), `{"turn": "t1", "rev": 1, "text": "x"}`)
		if device != tc.want {
			t.Fatalf("listening %v: device %q, want %q", tc.listening, device, tc.want)
		}
	}
	// A turn Mira did not see start belongs to none.
	var device = "x"
	c.opts.Handle = func(ev Event, d string) { device = d }
	c.dispatch(string(Reading), `{"turn": "unknown", "rev": 1}`)
	if device != "" {
		t.Fatalf("unknown turn at %q", device)
	}
}
