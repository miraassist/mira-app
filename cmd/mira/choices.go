package main

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira-app/internal/choices"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/conversation"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/store"
)

// Buttons on a private question and the taps that answer it
// (docs/spec/private-choices.md).

// threadAsks says whether a thread waits for its person's answer: for a
// yes, or for a choice among its proposals when its target names the field
// a chosen one goes into and that field is still empty (decision 2).
func (a *app) threadAsks(ctx context.Context, id string) ([]string, bool) {
	th, err := a.store.GetThread(ctx, id)
	if err != nil {
		return nil, false
	}
	if th.State == store.ThreadProposed {
		return nil, true
	}
	if chosen := a.threads.Chosen(th.Target); chosen != "" && store.ThreadOpen(th.State) && th.Fields[chosen] == "" {
		if props := choices.Proposals(th); len(props) > 0 {
			return props, true
		}
	}
	return nil, false
}

type choiceThreads struct{ a *app }

func (t choiceThreads) Get(ctx context.Context, id string) (store.Thread, error) {
	return t.a.store.GetThread(ctx, id)
}

func (t choiceThreads) Confirm(ctx context.Context, id string) (string, error) {
	return t.a.threads.ConfirmThread(ctx, id)
}

func (t choiceThreads) Cancel(ctx context.Context, id string) error {
	return t.a.threads.CancelThread(ctx, id)
}

func (t choiceThreads) Choose(ctx context.Context, id, proposal string) (string, error) {
	return t.a.threads.ChooseThread(ctx, id, proposal)
}

// startChoices hears the taps for the notifier's life.
func (a *app) startChoices(ctx context.Context) {
	if a.ha == nil || a.threads == nil {
		return
	}
	s := choices.New(choices.Options{HA: a.ha, Threads: choiceThreads{a}, Persons: a.persons,
		Tell: a.tellPrivately, Turn: a.choiceTurn, Lang: func() i18n.Lang { return a.lang }, Log: a.log.With("component", "choices")})
	a.goProactive(ctx, "choices", func(ctx context.Context) error {
		if err := s.Start(ctx); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
}

// tellPrivately leaves a private message for a person: a tap's outcome.
func (a *app) tellPrivately(ctx context.Context, person, text string) error {
	_, _, err := a.store.PutMessage(ctx, store.Message{ID: ulid.Make().String(), Kind: store.KindPrivate,
		Source: store.SourceTask, DedupeKey: "choice:" + ulid.Make().String(), Importance: 1, Receiver: person, Spoken: text})
	return err
}

// choiceTurn takes a typed answer in the app as a turn of the person, as the
// app's Assist would, and sends the answer back privately (decision 12).
func (a *app) choiceTurn(ctx context.Context, person, haUser, text string) error {
	if a.engine == nil {
		return fmt.Errorf("no conversation yet for %s's answer", person)
	}
	resp, err := a.engine.Handle(ctx, conversation.Request{Channel: config.ChannelVoice, ConversationID: "app-answer-" + person,
		HAUser: haUser, Text: text, Language: string(a.lang)})
	if err != nil {
		return err
	}
	return a.tellPrivately(ctx, person, resp.Text)
}
