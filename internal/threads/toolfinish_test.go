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

// choiceEnv is a target that asks privately with proposals, chosen into
// body (docs/spec/private-choices.md, decisions 18, 20).
func choiceEnv(t *testing.T) (*env, *[]string, *[]ranTool) {
	t.Helper()
	e := setup(t)
	var asked []string
	var ran []ranTool
	sk := skills.Skill{Name: "antwort", Description: "Antwortet", Target: &skills.Target{
		Fields:  []skills.TargetField{{Name: "to"}, {Name: "subject"}, {Name: "body"}, {Name: "proposals", Optional: true}},
		Private: true, Confirm: true, Chosen: "body",
		Finish: skills.Finish{Name: skills.FinishTool, Tool: "antwort__send"},
	}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{sk}, Clock: e.clk,
		RunTool: func(_ context.Context, name string, args json.RawMessage, thirdParty bool, _ string) (string, bool, error) {
			var a map[string]any
			_ = json.Unmarshal(args, &a)
			ran = append(ran, ranTool{name, a, thirdParty})
			return "Gesendet.", false, nil
		},
		AskPrivately: func(_ context.Context, th store.Thread, text string) error {
			asked = append(asked, th.Person+": "+th.Fields["proposals"])
			return nil
		}})
	return e, &asked, &ran
}

func (e *env) replyFinding(t *testing.T) store.Thread {
	t.Helper()
	return e.item(t, "antwort", "reply", "Maler: Donnerstag oder Freitag?",
		map[string]string{"to": "maler@example.org", "subject": "Re: Termin", "proposals": "Donnerstag passt.\nLieber Freitag."},
		e.clk.Now().Add(48*time.Hour))
}

// PC-14 New proposals are asked again privately
func TestPC14_NewProposalsAreAskedAgainPrivately(t *testing.T) {
	e, asked, ran := choiceEnv(t)
	ctx := context.Background()
	// Given Anna's thread waits for a choice and asks privately
	th := e.replyFinding(t)
	inv := e.annasTurn(t)
	if _, err := e.m.Take(ctx, inv, th.ID); err != nil {
		t.Fatal(err)
	}
	// When a turn writes new proposals into it
	out, err := e.m.Fill(ctx, inv, map[string]string{"proposals": "Montag passt.\nDienstag passt.\nMittwoch passt."})
	if err != nil {
		t.Fatal(err)
	}
	// Then the new proposals go to her privately with their buttons
	if len(*asked) != 1 || (*asked)[0] != "anna: Montag passt.\nDienstag passt.\nMittwoch passt." || !strings.Contains(out, "privat") {
		t.Fatalf("asked %q, the turn got %q", *asked, out)
	}
	if len(*ran) != 0 {
		t.Errorf("sent %+v", *ran)
	}
}

// PC-15 A chosen proposal is the yes
func TestPC15_AChosenProposalIsTheYes(t *testing.T) {
	e, asked, ran := choiceEnv(t)
	ctx := context.Background()
	// When Anna taps "Vorschlag 2 senden"
	th := e.replyFinding(t)
	if _, err := e.m.ChooseThread(ctx, th.ID, "Lieber Freitag."); err != nil {
		t.Fatal(err)
	}
	// Then the thread finishes with it, and no private yes/no question is left for it
	if len(*ran) != 1 || (*ran)[0].args["body"] != "Lieber Freitag." || len(*asked) != 0 {
		t.Fatalf("ran %+v asked %q", *ran, *asked)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadDone {
		t.Errorf("state %s", got.State)
	}
	// A turn that fills body with the second proposal: told it is the yes, not asked again.
	e2, asked2, _ := choiceEnv(t)
	th2 := e2.replyFinding(t)
	inv := e2.annasTurn(t)
	if _, err := e2.m.Take(ctx, inv, th2.ID); err != nil {
		t.Fatal(err)
	}
	out, err := e2.m.Fill(ctx, inv, map[string]string{"body": "Lieber Freitag."})
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked2) != 0 || !strings.Contains(out, "thread_confirm") {
		t.Errorf("asked %q, the turn got %q", *asked2, out)
	}
	// A body of the model's own (no proposal) still asks for the yes privately.
	e3, asked3, _ := choiceEnv(t)
	th3 := e3.replyFinding(t)
	inv3 := e3.annasTurn(t)
	if _, err := e3.m.Take(ctx, inv3, th3.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e3.m.Fill(ctx, inv3, map[string]string{"body": "Montag ginge auch."}); err != nil {
		t.Fatal(err)
	}
	if len(*asked3) != 1 {
		t.Errorf("asked %q", *asked3)
	}
}

// agentChoiceEnv: a target that asks privately whose proposals an agent
// writes from the mail's question (docs/spec/inbox.md, decision 44).
func agentChoiceEnv(t *testing.T, askErr error) (*env, *[]string, *[]string, *[]string) {
	t.Helper()
	e := setup(t)
	var asked, dispatched, told []string
	sk := skills.Skill{Name: "antwort", Description: "Antwortet", Target: &skills.Target{
		Fields: []skills.TargetField{{Name: "to"}, {Name: "subject"}, {Name: "question", Optional: true}, {Name: "body"},
			{Name: "proposals", Optional: true, Agent: "antwort.schreiben", Input: map[string]any{"input": "{{ question }}"}}},
		Private: true, Confirm: true, Chosen: "body",
		Finish: skills.Finish{Name: skills.FinishTool, Tool: "antwort__send"},
	}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{sk}, Clock: e.clk,
		RunTool: func(context.Context, string, json.RawMessage, bool, string) (string, bool, error) {
			return "ok", false, nil
		},
		Dispatch: func(_ context.Context, th store.Thread, into, agent, callID string, input json.RawMessage) error {
			dispatched = append(dispatched, into+" "+agent+" "+callID+" "+string(input))
			return nil
		},
		Deliver: func(_ context.Context, n store.Message, _ string) error {
			told = append(told, n.Receiver+": "+n.Spoken)
			return nil
		},
		AskPrivately: func(_ context.Context, th store.Thread, text string) error {
			if askErr != nil {
				return askErr
			}
			asked = append(asked, th.Person+": "+th.Fields["proposals"])
			return nil
		}})
	return e, &asked, &dispatched, &told
}

func (e *env) questionFinding(t *testing.T) store.Thread {
	t.Helper()
	th, err := e.m.StartAt(context.Background(), Begin{Target: "antwort", Person: "anna", Request: "Maler: Donnerstag oder Freitag?",
		Fields:   map[string]string{"to": "maler@example.org", "subject": "Re: Termin", "question": "Passt Ihnen Donnerstag oder Freitag?"},
		Deadline: e.clk.Now().Add(48 * time.Hour), Source: "mail:x:1"})
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// IN-65 A finding's field agent starts with its thread
func TestIN65_AFindingsFieldAgentStartsWithItsThread(t *testing.T) {
	e, asked, dispatched, _ := agentChoiceEnv(t, nil)
	// When a document gives Anna a finding of that kind, with the question filled
	th := e.questionFinding(t)
	// Then the agent starts with the thread, and nothing is asked yet
	if len(*dispatched) != 1 || !strings.HasPrefix((*dispatched)[0], "proposals antwort.schreiben ") ||
		!strings.Contains((*dispatched)[0], `"Passt Ihnen Donnerstag oder Freitag?"`) {
		t.Fatalf("dispatched %q", *dispatched)
	}
	if !e.m.AskFinding(context.Background(), th) || len(*asked) != 0 {
		t.Errorf("asked %q", *asked)
	}
}

// IN-66 The finding is asked privately when its proposals arrive
func TestIN66_TheFindingIsAskedPrivatelyWhenItsProposalsArrive(t *testing.T) {
	e, asked, dispatched, _ := agentChoiceEnv(t, nil)
	ctx := context.Background()
	th := e.questionFinding(t)
	call := strings.Fields((*dispatched)[0])[2]
	// When the agent's proposals arrive
	e.m.AgentResult(ctx, th.ID, "proposals", store.AgentRun{CallID: call, Agent: "antwort.schreiben", Status: "done",
		Output: "Donnerstag passt.\nLieber Freitag.\nIch melde mich."})
	// Then Anna is asked privately with them and their buttons
	if len(*asked) != 1 || (*asked)[0] != "anna: Donnerstag passt.\nLieber Freitag.\nIch melde mich." {
		t.Fatalf("asked %q", *asked)
	}
	// And when the agent fails, the thread tells Anna as its message
	e2, asked2, dispatched2, told2 := agentChoiceEnv(t, nil)
	th2 := e2.questionFinding(t)
	call2 := strings.Fields((*dispatched2)[0])[2]
	e2.m.AgentResult(ctx, th2.ID, "proposals", store.AgentRun{CallID: call2, Agent: "antwort.schreiben", Status: "failed", Error: "model down"})
	if len(*asked2) != 0 || len(*told2) != 1 || !strings.HasPrefix((*told2)[0], "anna: ") {
		t.Errorf("asked %q told %q", *asked2, *told2)
	}
	// Without a private channel, the proposals come as the thread's message.
	e3, _, dispatched3, told3 := agentChoiceEnv(t, errors.New("anna has no private channel"))
	th3 := e3.questionFinding(t)
	call3 := strings.Fields((*dispatched3)[0])[2]
	e3.m.AgentResult(ctx, th3.ID, "proposals", store.AgentRun{CallID: call3, Agent: "antwort.schreiben", Status: "done", Output: "A.\nB."})
	if len(*told3) != 1 || !strings.Contains((*told3)[0], "A.\nB.") {
		t.Errorf("told %q", *told3)
	}
}
