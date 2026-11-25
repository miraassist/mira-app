package conversation_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/conversation"
	"github.com/miraassist/mira-app/internal/llm"
	"github.com/miraassist/mira-app/internal/prompt"
	"github.com/miraassist/mira-app/internal/sidechannel"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/testsupport/fakellm"
)

// Speculative turns (docs/spec/speculative-turns.md): the kitchen satellite
// sat-kueche, a side-channel turn "s1".

const sat = "sat-kueche"

func reading(turn string, rev int, text string, complete float64) sidechannel.Event {
	return sidechannel.Event{Kind: sidechannel.Reading, Turn: turn, Rev: rev, Text: text, Complete: &complete, AudioS: 2.5}
}

// atSatellite is a turn Home Assistant sends from the kitchen satellite.
func atSatellite(t *testing.T, h *harness, conv, text string, stream func(conversation.Piece)) conversation.Response {
	t.Helper()
	resp, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice,
		ConversationID: conv, DeviceID: sat, Text: text, Language: "de", Stream: stream})
	if err != nil {
		t.Fatalf("Handle(%q): %v", text, err)
	}
	return resp
}

// warm is an earlier turn at the satellite: the language Home Assistant
// sends there is known from it (decision 1).
func warm(t *testing.T, h *harness) {
	t.Helper()
	h.llm.Push(fakellm.Step{Content: "Gern."})
	atSatellite(t, h, "c0", "Danke", nil)
	h.llm.Forget()
}

func await(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second): // real time: a test's own bound on a hang
		t.Fatalf("no %s", what)
	}
}

func requestsWith(h *harness, text string) int {
	n := 0
	for _, r := range h.llm.Requests() {
		if strings.Contains(lastContent(r.Request.Messages), text) {
			n++
		}
	}
	return n
}

func TestSC06_AFinishedSoundingReadingStartsARun(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	taken := make(chan struct{})
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr.", Taken: taken})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.8), sat)
	await(t, taken, "run")
	layer7 := lastContent(h.llm.Last().Request.Messages)
	if !strings.Contains(layer7, "Wie spät ist es?") || !strings.Contains(layer7, "Küche") {
		t.Fatalf("the run's turn:\n%s", layer7)
	}
}

func TestSC20_EveryReadingStartsARun(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	taken := make(chan struct{})
	h.llm.Push(fakellm.Step{Content: "Welches Zimmer?", Taken: taken})
	h.engine.Speculate(reading("s1", 1, "Schalte das Licht im", 0.2), sat)
	await(t, taken, "run")
	if requestsWith(h, "Schalte das Licht im") != 1 {
		t.Fatal("no run on the unfinished reading")
	}
}

func TestSC08_AStaleReadingEndsItsRun(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	taken, hold := make(chan struct{}), make(chan struct{})
	defer close(hold)
	h.llm.Push(fakellm.Step{Content: "Welches Zimmer?", Taken: taken, Hold: hold})
	h.engine.Speculate(reading("s1", 1, "Schalte das Licht im", 0.8), sat)
	await(t, taken, "run")
	ended := h.logs.Await("speculation", "ended=stale")
	h.engine.Speculate(sidechannel.Event{Kind: sidechannel.ReadingStale, Turn: "s1", Rev: 1}, sat)
	await(t, ended, "log line of the stale run")

	// Nothing is kept: the turn asks the model itself.
	h.llm.Push(fakellm.Step{Content: "Licht ist an."})
	atSatellite(t, h, "c1", "Schalte das Licht im", nil)
	if n := requestsWith(h, "Schalte das Licht im"); n != 2 {
		t.Fatalf("%d requests, want the run's and the turn's", n)
	}
}

func TestSC09_ARunStopsAtAToolThatDoesNotOnlyRead(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__HassTurnOn", `{"name":"Küchenlicht"}`)}})
	h.engine.Speculate(reading("s1", 1, "Mach das Licht in der Küche an", 0.8), sat)
	waitRequests(t, h, 1)
	time.Sleep(50 * time.Millisecond) // real time: room for a wrong second round or tool call
	if n := len(h.haCalls("HassTurnOn")); n != 0 {
		t.Fatalf("the run turned the light on %d times", n)
	}
	if n := len(h.llm.Requests()); n != 1 {
		t.Fatalf("%d requests; the run went on after a writing call", n)
	}
}

func TestSC10_ARunRunsAReadOnlyTool(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: "Das Küchenlicht ist aus."},
	)
	h.engine.Speculate(reading("s1", 1, "Ist das Licht in der Küche an?", 0.8), sat)
	waitRequests(t, h, 2)
	if n := len(h.haCalls("GetLiveContext")); n != 1 {
		t.Fatalf("GetLiveContext ran %d times", n)
	}
	if got := lastToolMessage(h.llm.Last().Request); !strings.Contains(got, "Küchenlicht aus") {
		t.Fatalf("the second round lacks the result: %q", got)
	}
}

func TestSC11_NoRunWhileAConfirmationWaits(t *testing.T) {
	h := newHarness(t)
	h.llm.Push(fakellm.Step{Content: "Soll ich die Haustür aufschließen?"})
	if !atSatellite(t, h, "c1", "Schließ die Haustür auf", nil).ContinueConversation {
		t.Fatal("the question did not keep the satellite listening")
	}
	ctx := context.Background()
	if err := h.store.PutConfirmation(ctx, store.Confirmation{ID: "k1", SessionKey: "voice:c1", ToolName: "ha__HassTurnOn",
		Arguments: `{}`, Summary: "Haustür aufschließen"}, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	h.llm.Forget()
	skipped := h.logs.Await("speculation", "ended=\"skipped: a confirmation waits\"")
	h.engine.Speculate(reading("s1", 1, "Ja", 0.9), sat)
	await(t, skipped, "log line")
	if has, err := h.store.HasConfirmation(ctx, "voice:c1"); err != nil || !has {
		t.Fatalf("the confirmation is gone (%v)", err)
	}
	if n := len(h.llm.Requests()); n != 0 {
		t.Fatalf("%d model requests", n)
	}
}

func TestSC12_TheSameRequestTakesTheRunsRound(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr."})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.8), sat)
	waitRequests(t, h, 1)

	var mu sync.Mutex
	var streamed strings.Builder
	taken := h.logs.Await("speculation", "ended=taken", "rounds_taken=1")
	resp := atSatellite(t, h, "c1", "Wie spät ist es?", func(p conversation.Piece) {
		mu.Lock()
		streamed.WriteString(p.Text)
		mu.Unlock()
	})
	if resp.Text != "Es ist 7:12 Uhr." {
		t.Fatalf("answer %q", resp.Text)
	}
	if n := len(h.llm.Requests()); n != 1 {
		t.Fatalf("%d model requests, want only the run's", n)
	}
	mu.Lock()
	if got := streamed.String(); !strings.Contains(got, "Es ist 7:12 Uhr.") {
		t.Fatalf("streamed %q", got)
	}
	mu.Unlock()
	await(t, taken, "log line of the taken run")
}

func TestSC13_ADifferentTextCallsTheModel(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(fakellm.Step{Content: "Welches Zimmer?"}, fakellm.Step{Content: "Licht ist an."})
	h.engine.Speculate(reading("s1", 1, "Schalte das Licht im", 0.8), sat)
	waitRequests(t, h, 1)
	ended := h.logs.Await("speculation", "ended=no_match")
	resp := atSatellite(t, h, "c1", "Schalte das Licht im Wohnzimmer an", nil)
	if resp.Text != "Licht ist an." || len(h.llm.Requests()) != 2 {
		t.Fatalf("answer %q after %d requests", resp.Text, len(h.llm.Requests()))
	}
	await(t, ended, "no_match line")
}

func TestSC14_ATakenRoundsWritingCallRunsInTheTurn(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__HassTurnOn", `{"name":"Küchenlicht"}`)}},
		fakellm.Step{Content: "Das Küchenlicht ist an."},
	)
	h.engine.Speculate(reading("s1", 1, "Mach das Licht in der Küche an", 0.8), sat)
	waitRequests(t, h, 1)
	resp := atSatellite(t, h, "c1", "Mach das Licht in der Küche an", nil)
	if resp.Text != "Das Küchenlicht ist an." {
		t.Fatalf("answer %q", resp.Text)
	}
	if n := len(h.haCalls("HassTurnOn")); n != 1 {
		t.Fatalf("the light was turned on %d times", n)
	}
	if n := len(h.llm.Requests()); n != 2 {
		t.Fatalf("%d requests, want the run's first round and the turn's second", n)
	}
}

func TestSC15_TheTurnWaitsForARoundOnItsWay(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	taken, hold := make(chan struct{}), make(chan struct{})
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr.", Taken: taken, Hold: hold})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.8), sat)
	await(t, taken, "run")
	got := make(chan conversation.Response)
	go func() { got <- atSatellite(t, h, "c1", "Wie spät ist es?", nil) }()
	await(t, h.logs.Await("turn"), "the turn")
	close(hold)
	resp := <-got
	if resp.Text != "Es ist 7:12 Uhr." || len(h.llm.Requests()) != 1 {
		t.Fatalf("answer %q after %d requests", resp.Text, len(h.llm.Requests()))
	}
}

func TestSC16_AnotherPersonsRequestIsNotTaken(t *testing.T) {
	h := newHarness(t)
	voice := "anna"
	h.identify = func(ctx context.Context, req conversation.Request) (string, string) {
		if req.Speculative {
			return req.Voice, prompt.ByVoice
		}
		return voice, prompt.ByVoice
	}
	warm(t, h) // anna's voice at the satellite
	voice = "ben"
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr, Anna."}, fakellm.Step{Content: "Es ist 7:12 Uhr, Ben."})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.8), sat)
	waitRequests(t, h, 1)
	if l := lastContent(h.llm.Last().Request.Messages); !strings.Contains(l, "anna") {
		t.Fatalf("the run did not guess anna:\n%s", l)
	}
	resp := atSatellite(t, h, "c1", "Wie spät ist es?", nil)
	if resp.Text != "Es ist 7:12 Uhr, Ben." {
		t.Fatalf("answer %q", resp.Text)
	}
}

func TestSC17_ATurnWaitingForTheModelsPlaceEndsTheRun(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) {
		m := c.Main()
		m.Parallel = 1
		c.Models[config.MainModel] = m
	})
	warm(t, h)
	taken, hold := make(chan struct{}), make(chan struct{})
	defer close(hold)
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr.", Taken: taken, Hold: hold}, fakellm.Step{Content: "Hallo."})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.8), sat)
	await(t, taken, "run")
	yielded := h.logs.Await("speculation", "ended=yielded")
	resp := h.say(t, config.ChannelTelegram, "42", "Hallo")
	if resp.Text != "Hallo." {
		t.Fatalf("answer %q", resp.Text)
	}
	await(t, yielded, "the run's yield")
}

// waitRequests waits until the model has had n requests.
func waitRequests(t *testing.T, h *harness, n int) {
	t.Helper()
	for i := 0; len(h.llm.Requests()) < n; i++ {
		if i == 1000 {
			t.Fatalf("%d requests, want %d", len(h.llm.Requests()), n)
		}
		<-time.After(5 * time.Millisecond) // real time: polling the fake model's record, at most 5 s
	}
}

func TestSC18_AReadingOfTheWakeWordsTailStartsNothing(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	taken := make(chan struct{})
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr.", Taken: taken})
	tail := reading("s1", 1, "irá.", 0.9)
	tail.AudioS = 0.6
	h.engine.Speculate(tail, sat)
	h.engine.Speculate(reading("s1", 2, "Wie spät ist es?", 0.9), sat)
	await(t, taken, "run")
	if n := requestsWith(h, "irá."); n != 0 {
		t.Fatalf("the wake word's tail started %d runs", n)
	}
}

func TestSC19_ADifferingRequestSaysWhere(t *testing.T) {
	h := newHarness(t)
	warm(t, h)
	h.llm.Push(fakellm.Step{Content: "Welches Zimmer?"}, fakellm.Step{Content: "Licht ist an."})
	h.engine.Speculate(reading("s1", 1, "Schalte das Licht im", 0.8), sat)
	waitRequests(t, h, 1)
	differed := h.logs.Await("speculation request differed", "round=0", "Schalte das Licht im Wohnzimmer")
	atSatellite(t, h, "c1", "Schalte das Licht im Wohnzimmer an", nil)
	await(t, differed, "the line saying where the requests differ")
}

func TestSC21_TheSatellitesVoiceIsKeptAcrossARestart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// What an earlier Mira kept: anna's voice, German.
	if err := h.store.SetKV(ctx, "speculation."+sat, `{"voice":"anna","lang":"de"}`); err != nil {
		t.Fatal(err)
	}
	h.identify = func(ctx context.Context, req conversation.Request) (string, string) {
		if req.Speculative {
			return req.Voice, prompt.ByVoice
		}
		return "anna", prompt.ByVoice
	}
	h.llm.Push(fakellm.Step{Content: "Es ist 7:12 Uhr, Anna."})
	h.engine.Speculate(reading("s1", 1, "Wie spät ist es?", 0.9), sat)
	waitRequests(t, h, 1)
	resp := atSatellite(t, h, "c1", "Wie spät ist es?", nil)
	if resp.Text != "Es ist 7:12 Uhr, Anna." || len(h.llm.Requests()) != 1 {
		t.Fatalf("answer %q after %d requests: the kept voice was not used", resp.Text, len(h.llm.Requests()))
	}

	// A turn whose voice names ben keeps ben for the next restart.
	h.identify = func(ctx context.Context, req conversation.Request) (string, string) { return "ben", prompt.ByVoice }
	h.llm.Push(fakellm.Step{Content: "Gern."})
	atSatellite(t, h, "c2", "Danke", nil)
	got, err := h.store.GetKV(ctx, "speculation."+sat)
	if err != nil || got != `{"voice":"ben","lang":"de"}` {
		t.Fatalf("kept %q (%v)", got, err)
	}
}
