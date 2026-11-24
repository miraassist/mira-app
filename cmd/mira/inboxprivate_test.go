package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/inbox"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/store/storetest"
	"github.com/miraassist/mira-app/internal/testsupport/logcap"
	"github.com/miraassist/mira-app/internal/threads"
)

// docs/spec/inbox.md, decision 43: a finding of a target that asks
// privately goes to the person privately at its start.

const replySkillMD = `---
name: antwort
description: Antwortet auf Mails
metadata:
  mira:
    takes: reply
    target:
      private: true
      confirm: true
      chosen: body
      fields:
        - name: to
          description: Empfänger
        - name: subject
          description: Betreff
        - name: body
          description: Text
        - name: proposals
          description: Vorschläge
          optional: true
      finish:
        action: notify.notify
---
# Antwort

## Ansage

Sag, wer eine Antwort möchte, und lies die Vorschläge vor.
`

// privateInboxApp is an app with a skill taking the kind reply whose target
// asks privately; askErr is what the private channel answers.
func privateInboxApp(t *testing.T, askErr error) (*app, *[]string, interface{ String() string }) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "antwort")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The finish is a Home Assistant action here: the test is about the asking, not the finish.
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(replySkillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{filepath.Dir(dir)}, map[string]map[string]any{"antwort": nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	takers, _ := kindTakers(loaded)
	store.SetTakers(takers)
	t.Cleanup(func() { store.SetTakers(nil) })
	log, lines := logcap.Logger()
	var asked []string
	a := &app{store: st, log: slog.New(slog.NewTextHandler(io.Discard, nil)), lang: i18n.DE}
	a.threads = threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: log,
		AskPrivately: func(_ context.Context, th store.Thread, text string) error {
			if askErr != nil {
				return askErr
			}
			asked = append(asked, th.Person+": "+text)
			return nil
		}})
	return a, &asked, lines
}

func askReply(t *testing.T, a *app) {
	t.Helper()
	q := inbox.Question{Person: "anna", Doc: inbox.Doc{Key: "mail:x:handwerker", Title: "Termin", Ref: "Mail"},
		Kind: "reply", Line: "Handwerker fragt: passt Donnerstag?",
		Fields: map[string]string{"to": "info@example.com", "subject": "Re: Termin",
			"proposals": "Donnerstag passt.\nLieber Freitag.\nIch melde mich."},
		Deadline: a.store.Now().Add(48 * time.Hour)}
	if err := a.inboxAsk(context.Background(), q); err != nil {
		t.Fatal(err)
	}
}

func annasMessages(t *testing.T, a *app) []store.Message {
	t.Helper()
	ms, err := a.store.ListMessages(context.Background(), store.MessageFilter{
		States: []string{store.MessagePending}, Receiver: "anna"})
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// IN-63 A finding of a private target is asked privately at once
func TestIN63_AFindingOfAPrivateTargetIsAskedPrivatelyAtOnce(t *testing.T) {
	a, asked, _ := privateInboxApp(t, nil)
	// When a document gives Anna a finding of the kind reply with three proposals
	askReply(t, a)
	// Then Anna gets a private message of the thread at once, with the request and the fields
	if len(*asked) != 1 || !strings.HasPrefix((*asked)[0], "anna: Handwerker fragt: passt Donnerstag?\nto: info@example.com\nsubject: Re: Termin") {
		t.Fatalf("asked %q", *asked)
	}
	// And no entry goes into her message, nothing is announced
	if ms := annasMessages(t, a); len(ms) != 0 {
		t.Errorf("messages %+v", ms)
	}
}

// IN-64 Without a private channel the finding is announced as before
func TestIN64_WithoutAPrivateChannelTheFindingIsAnnouncedAsBefore(t *testing.T) {
	a, _, logs := privateInboxApp(t, errors.New("anna has no private channel"))
	askReply(t, a)
	ms := annasMessages(t, a)
	if len(ms) != 1 || ms[0].Kind != "reply" || len(ms[0].Entries) != 1 {
		t.Fatalf("messages %+v", ms)
	}
	if !strings.Contains(logs.String(), "a private finding is announced") || !strings.Contains(logs.String(), "no private channel") {
		t.Errorf("log:\n%s", logs.String())
	}
}
