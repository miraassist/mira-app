package threads

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/testsupport/logcap"
)

// docs/spec/inbox.md, section 9, and docs/spec/threads.md, finishes: a
// skill's own kind of finding finishes with a tool of the skill.

type ranTool struct {
	name       string
	args       map[string]any
	thirdParty bool
}

// replySkill takes a kind of its own and finishes with its tool.
func replySkill() skills.Skill {
	return skills.Skill{Name: "antwort", Description: "Antwortet", Target: &skills.Target{
		Fields: []skills.TargetField{{Name: "to"}, {Name: "subject"}, {Name: "body"}},
		Finish: skills.Finish{Name: skills.FinishTool, Tool: "antwort__send"},
	}}
}

func toolEnv(t *testing.T, answer string, isError bool) (*env, *[]ranTool) {
	t.Helper()
	e := setup(t)
	var ran []ranTool
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{replySkill()}, Clock: e.clk,
		RunTool: func(_ context.Context, name string, args json.RawMessage, thirdParty bool, _ string) (string, bool, error) {
			var a map[string]any
			_ = json.Unmarshal(args, &a)
			ran = append(ran, ranTool{name, a, thirdParty})
			return answer, isError, nil
		}})
	return e, &ran
}

// IN-61 A finding of the skill's kind finishes with its tool
func TestIN61_AFindingOfTheSkillsKindFinishesWithItsTool(t *testing.T) {
	e, ran := toolEnv(t, "Gesendet an handwerker@example.org.", false)
	ctx := context.Background()
	// Given a thread of the kind reply waits for Anna's yes
	th := e.item(t, "antwort", "reply", "Handwerker: Termin Donnerstag?",
		map[string]string{"to": "handwerker@example.org", "subject": "Re: Termin", "body": "Donnerstag passt."},
		e.clk.Now().Add(48*time.Hour))
	inv := e.annasTurn(t)
	if _, err := e.m.Take(ctx, inv, th.ID); err != nil {
		t.Fatal(err)
	}
	// The model takes the proposal as it came (as for termin, IN-50).
	if _, err := e.m.Fill(ctx, inv, map[string]string{"body": "Donnerstag passt."}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadProposed || len(*ran) != 0 {
		t.Fatalf("before the yes: state %s, ran %+v", got.State, *ran)
	}
	// When she says yes
	text, err := e.m.Confirm(ctx, inv)
	if err != nil {
		t.Fatal(err)
	}
	// Then the skill's tool runs once with the thread's fields as its arguments, and its answer is the thread's message
	if len(*ran) != 1 || (*ran)[0].name != "antwort__send" || (*ran)[0].args["to"] != "handwerker@example.org" ||
		(*ran)[0].args["body"] != "Donnerstag passt." {
		t.Fatalf("ran %+v", *ran)
	}
	if text == "" || !strings.Contains(text, "Gesendet an handwerker@example.org.") {
		t.Errorf("the thread's message: %q", text)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadDone {
		t.Errorf("state %s", got.State)
	}
}

// IN-62 A finding's tool call counts as after third-party text
func TestIN62_AFindingsToolCallCountsAsAfterThirdPartyText(t *testing.T) {
	e, ran := toolEnv(t, "ok", false)
	ctx := context.Background()
	th := e.item(t, "antwort", "reply", "Schule: Rückmeldung",
		map[string]string{"to": "schule@example.org", "subject": "Re: Ausflug", "body": "Ja."}, e.clk.Now().Add(48*time.Hour))
	inv := e.annasTurn(t)
	e.m.Take(ctx, inv, th.ID)
	if _, err := e.m.Fill(ctx, inv, map[string]string{"body": "Ja."}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Confirm(ctx, inv); err != nil {
		t.Fatal(err)
	}
	// When the finishing tool is called, then its call carries third_party_text true
	if len(*ran) != 1 || !(*ran)[0].thirdParty {
		t.Fatalf("ran %+v", *ran)
	}
	// A tool's error is the thread's error.
	e2, _ := toolEnv(t, "the server refused the address", true)
	th2 := e2.item(t, "antwort", "reply", "x", map[string]string{"to": "a@b.de", "subject": "s", "body": "b"}, e2.clk.Now().Add(time.Hour))
	inv2 := e2.annasTurn(t)
	e2.m.Take(ctx, inv2, th2.ID)
	if _, err := e2.m.Fill(ctx, inv2, map[string]string{"body": "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e2.m.Confirm(ctx, inv2); err == nil || !strings.Contains(err.Error(), "refused the address") {
		t.Errorf("a tool error: %v", err)
	}
}

// ChooseThread puts a tapped proposal into the target's chosen field and
// confirms (docs/spec/private-choices.md, step 3).
func TestAChosenProposalFillsTheFieldAndFinishes(t *testing.T) {
	e := setup(t)
	var ran []ranTool
	sk := skills.Skill{Name: "antwort", Description: "Antwortet", Target: &skills.Target{
		Fields: []skills.TargetField{{Name: "to"}, {Name: "proposals"}, {Name: "body"}},
		Chosen: "body",
		Finish: skills.Finish{Name: skills.FinishTool, Tool: "antwort__send"},
	}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{sk}, Clock: e.clk,
		RunTool: func(_ context.Context, name string, args json.RawMessage, thirdParty bool, _ string) (string, bool, error) {
			var a map[string]any
			_ = json.Unmarshal(args, &a)
			ran = append(ran, ranTool{name, a, thirdParty})
			return "Gesendet.", false, nil
		}})
	th := e.item(t, "antwort", "reply", "Handwerker: Termin?", map[string]string{"to": "h@example.org",
		"proposals": "Donnerstag passt.\nLieber Freitag."}, e.clk.Now().Add(48*time.Hour))
	if e.m.Chosen("antwort") != "body" {
		t.Fatalf("chosen %q", e.m.Chosen("antwort"))
	}
	out, err := e.m.ChooseThread(context.Background(), th.ID, "Lieber Freitag.")
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0].args["body"] != "Lieber Freitag." || !ran[0].thirdParty || !strings.Contains(out, "Gesendet.") {
		t.Fatalf("ran %+v out %q", ran, out)
	}
	if got, _ := e.st.GetThread(context.Background(), th.ID); got.State != store.ThreadDone {
		t.Errorf("state %s", got.State)
	}
}

// privateEnv is a thread target that asks privately, and what it sent.
func privateEnv(t *testing.T, askErr error) (*env, *[]string, interface{ String() string }) {
	t.Helper()
	e := setup(t)
	log, lines := logcap.Logger()
	var asked []string
	sk := skills.Skill{Name: "antwort", Description: "Antwortet", Target: &skills.Target{
		Fields:  []skills.TargetField{{Name: "to"}, {Name: "subject"}, {Name: "body"}},
		Private: true, Confirm: true,
		Finish: skills.Finish{Name: skills.FinishTool, Tool: "antwort__send"},
	}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{sk}, Clock: e.clk, Logger: log,
		RunTool: func(context.Context, string, json.RawMessage, bool, string) (string, bool, error) {
			return "ok", false, nil
		},
		AskPrivately: func(_ context.Context, th store.Thread, text string) error {
			if askErr != nil {
				return askErr
			}
			asked = append(asked, th.Person+": "+text)
			return nil
		}})
	return e, &asked, lines
}

// PC-09 A thread of a target that asks privately proposes over the private channel
func TestPC09_AThreadOfATargetThatAsksPrivatelyProposesOverThePrivateChannel(t *testing.T) {
	e, asked, _ := privateEnv(t, nil)
	ctx := context.Background()
	inv := e.annasTurn(t)
	// Given a target with private true and Anna's thread of it complete
	if _, err := e.m.Start(ctx, inv, "antwort", "Mail an den Handwerker"); err != nil {
		t.Fatal(err)
	}
	out, err := e.m.Fill(ctx, inv, map[string]string{"to": "h@example.org", "subject": "Termin",
		"body": "Hallo,\nDonnerstag passt uns gut.\nViele Grüße"})
	if err != nil {
		t.Fatal(err)
	}
	// Then its proposal goes to Anna as a private message of the thread, with the request and the fields
	if len(*asked) != 1 || !strings.Contains((*asked)[0], "anna: Mail an den Handwerker\nto: h@example.org\nsubject: Termin\n\nHallo,") {
		t.Fatalf("asked %q", *asked)
	}
	// And the turn is told that the question went privately, not the proposal
	if !strings.Contains(out, "privat") || strings.Contains(out, "Donnerstag") {
		t.Errorf("the turn got %q", out)
	}
}

// PC-10 Without a person the proposal is said in the turn
func TestPC10_WithoutAPersonTheProposalIsSaidInTheTurn(t *testing.T) {
	e, _, logs := privateEnv(t, errors.New("anna has no private channel"))
	ctx := context.Background()
	inv := e.annasTurn(t)
	if _, err := e.m.Start(ctx, inv, "antwort", "Mail"); err != nil {
		t.Fatal(err)
	}
	out, err := e.m.Fill(ctx, inv, map[string]string{"to": "h@example.org", "subject": "Termin", "body": "Donnerstag passt."})
	if err != nil {
		t.Fatal(err)
	}
	// Then its proposal is said in the turn as for any thread, and the log names why
	if !strings.Contains(out, "Donnerstag passt.") || !strings.Contains(logs.String(), "a private thread proposes in the turn") {
		t.Errorf("out %q\nlog:\n%s", out, logs.String())
	}
}
