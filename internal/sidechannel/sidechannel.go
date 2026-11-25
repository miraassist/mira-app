// Package sidechannel reads a side channel: a service beside the
// speech-to-text that reports, as Server-Sent Events, what is being said
// while a satellite still listens (docs/spec/speculative-turns.md). Mira only
// reads it; without its URL there is no client.
package sidechannel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/excerpt"
	"github.com/miraassist/mira-app/internal/retry"
)

// Kind is what an event reports.
type Kind string

const (
	// TurnStarted: Home Assistant opened a turn at the speech-to-text.
	TurnStarted Kind = "turn.started"
	// SpeechStarted: speech is heard, also again after a pause.
	SpeechStarted Kind = "speech.started"
	// SpeechPaused: a pause after speech.
	SpeechPaused Kind = "speech.paused"
	// Reading: the audio up to a pause was read.
	Reading Kind = "reading"
	// ReadingStale: the user went on after the reading Rev.
	ReadingStale Kind = "reading.stale"
	// TurnFinal: the transcript that goes to Home Assistant.
	TurnFinal Kind = "turn.final"
)

// Event is one event of the side channel. Fields an event does not carry
// stay zero; Complete is nil without a guess.
type Event struct {
	Kind Kind
	// Turn is the side channel's own id of the turn.
	Turn string
	// Sent is when the side channel sent the event.
	Sent time.Time
	// Rev numbers the readings of a turn (reading, reading.stale,
	// turn.final).
	Rev  int
	Text string
	// Complete is the side channel's guess that the user is done, 0 to 1.
	Complete *float64
	// AudioS is how much of the turn's audio a reading read, in seconds;
	// AtS where a speech event is in it.
	AudioS, AtS float64
	// Source says where turn.final's text comes from: "speculative" (the
	// reading Rev) or "fresh".
	Source string
}

// wire is an event as the side channel sends it.
type wire struct {
	Turn     string   `json:"turn"`
	T        float64  `json:"t"`
	Rev      int      `json:"rev"`
	Text     string   `json:"text"`
	Complete *float64 `json:"complete"`
	AudioS   float64  `json:"audio_s"`
	AtS      float64  `json:"at_s"`
	Source   string   `json:"source"`
}

// Policy is the reconnect: never given up, exponential from 1 s to 60 s
// (docs/spec/speculative-turns.md, Fails).
var Policy = retry.Policy{First: time.Second, Max: time.Minute, Growth: retry.Exponential}

// keptTurns is how many side-channel turns keep their satellite: more than
// satellites can listen at once.
const keptTurns = 32

// Options are what a client needs.
type Options struct {
	// URL is the side channel's events, e.g. http://host:10312/api/events.
	URL    string
	HTTP   *http.Client
	Clock  clock.Clock
	Logger *slog.Logger
	// Listening are the device ids of the satellites in listening now.
	Listening func() []string
	// Handle is told of each event with the satellite its turn belongs to
	// ("" for none). It runs on the read loop and must not block.
	Handle func(ev Event, device string)
}

// Client keeps one subscription.
type Client struct {
	opts  Options
	clock clock.Clock
	log   *slog.Logger
	// device is the satellite of each side-channel turn ("" for none),
	// order their ids, oldest first. Only Run's goroutine touches them.
	device map[string]string
	order  []string
}

// New returns a client, or nil when the URL is empty: speculation is off.
func New(opts Options) *Client {
	opts.URL = strings.TrimSpace(opts.URL)
	if opts.URL == "" {
		return nil
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Listening == nil {
		opts.Listening = func() []string { return nil }
	}
	if opts.Handle == nil {
		opts.Handle = func(Event, string) {}
	}
	return &Client{opts: opts, clock: clock.Or(opts.Clock), log: opts.Logger.With("component", "sidechannel"),
		device: map[string]string{}}
}

// Run keeps the subscription until ctx ends or the side channel answers
// with an error no reconnect can heal; that error is logged and returned.
func (c *Client) Run(ctx context.Context) error {
	p := Policy
	p.Clock = c.clock
	failed := 0
	var lastErr string
	for {
		connected, err := c.subscribe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			if failed > 0 {
				c.log.Info("side channel connected again", "url", c.opts.URL, "after_attempts", failed)
			}
			failed, lastErr = 0, ""
		}
		if !retry.Retryable(err) {
			c.log.Error("side channel refused; no speculative runs until its setting changes", "url", c.opts.URL, "error", err)
			return err
		}
		failed++
		wait := p.Wait(failed)
		if err.Error() != lastErr {
			c.log.Warn("side channel lost; subscribing again", "url", c.opts.URL, "attempt", failed,
				"wait", wait.String(), "error", err)
			lastErr = err.Error()
		}
		select {
		case <-c.clock.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// subscribe reads one connection until it ends. connected says whether the
// side channel answered with its stream; the error is why it ended, marked
// fatal where another attempt cannot help.
func (c *Client) subscribe(ctx context.Context) (connected bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.URL, nil)
	if err != nil {
		return false, retry.Fatal(fmt.Errorf("side channel url %q: %w", c.opts.URL, err))
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		head := make([]byte, 200)
		n, _ := resp.Body.Read(head)
		err := fmt.Errorf("answered %s: %q", resp.Status, excerpt.Bytes(head[:n]))
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
			return false, err
		}
		return false, retry.Fatal(err)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/event-stream" {
		head := make([]byte, 200)
		n, _ := resp.Body.Read(head)
		return false, retry.Fatal(fmt.Errorf("answered %q instead of text/event-stream: %q",
			resp.Header.Get("Content-Type"), excerpt.Bytes(head[:n])))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var kind string
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if kind != "" || data.Len() > 0 {
				c.dispatch(kind, data.String())
			}
			kind = ""
			data.Reset()
		case strings.HasPrefix(line, ":"):
			// A comment: the side channel's ping.
		case strings.HasPrefix(line, "event:"):
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return true, fmt.Errorf("stream broke: %w", err)
	}
	return true, errors.New("stream ended")
}

// dispatch hands one event on; one that does not parse is left out with a
// warning.
func (c *Client) dispatch(kind, data string) {
	var w wire
	if err := json.Unmarshal([]byte(data), &w); err != nil || w.Turn == "" {
		if err == nil {
			err = errors.New("no turn")
		}
		c.log.Warn("side channel event not understood; left out", "event", kind, "data", excerpt.Of(data), "error", err)
		return
	}
	ev := Event{Kind: Kind(kind), Turn: w.Turn, Rev: w.Rev, Text: w.Text, Complete: w.Complete,
		AudioS: w.AudioS, AtS: w.AtS, Source: w.Source}
	if w.T > 0 {
		sec, frac := math.Modf(w.T)
		ev.Sent = time.Unix(int64(sec), int64(frac*1e9))
	}
	if ev.Kind == TurnStarted {
		c.note(ev.Turn)
	}
	c.opts.Handle(ev, c.device[ev.Turn])
}

// note gives a new side-channel turn the one satellite in listening, or none
// (docs/spec/speculative-turns.md, decision 1).
func (c *Client) note(turn string) {
	device := ""
	if l := c.opts.Listening(); len(l) == 1 {
		device = l[0]
	} else {
		c.log.Debug("side channel turn without one listening satellite", "turn", turn, "listening", len(l))
	}
	if _, ok := c.device[turn]; !ok {
		c.order = append(c.order, turn)
	}
	c.device[turn] = device
	for len(c.order) > keptTurns {
		delete(c.device, c.order[0])
		c.order = c.order[1:]
	}
}
