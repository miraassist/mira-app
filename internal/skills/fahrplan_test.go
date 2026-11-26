package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/startools"
)

// The shipped skill fahrplan (docs/spec/fahrplan.md). The answers are cut from
// dbf.finalrewind.org's, recorded on 2026-11-26.

// dbfFake answers every request with one status and body and records the
// addresses asked.
type dbfFake struct {
	status int
	body   string
	err    error
	asked  []*url.URL
}

func (f *dbfFake) Request(_ context.Context, _, _, raw string, _ []byte) (int, []byte, error) {
	u, _ := url.Parse(raw)
	f.asked = append(f.asked, u)
	if f.err != nil {
		return 0, nil, f.err
	}
	return f.status, []byte(f.body), nil
}

// 2026-11-26 13:50 UTC, 14:50 in Berlin.
var dbfNow = time.Unix(1795701000, 0).UTC()

type fahrplanRun struct {
	out    map[string]any
	err    error
	events []startools.Event
}

func runAbfahrten(t *testing.T, f *dbfFake, settings map[string]any, args map[string]string, now time.Time) fahrplanRun {
	t.Helper()
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"fahrplan": settings})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("fahrplan did not load: %v", problems)
	}
	if loaded[0].Starlark == nil || len(loaded[0].API) != 1 {
		t.Fatalf("fahrplan: starlark %v api %v", loaded[0].Starlark, loaded[0].API)
	}
	raw, _ := json.Marshal(args)
	res := loaded[0].Starlark.Call(context.Background(), "abfahrten", raw, startools.Env{Requester: f, Now: func() time.Time { return now }})
	run := fahrplanRun{err: res.Err, events: res.Events}
	if res.Err == nil {
		if err := json.Unmarshal([]byte(res.Text), &run.out); err != nil {
			t.Fatalf("abfahrten: %q", res.Text)
		}
	}
	return run
}

func departures(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	list, _ := out["abfahrten"].([]any)
	var deps []map[string]any
	for _, d := range list {
		deps = append(deps, d.(map[string]any))
	}
	return deps
}

// hafasPulverturm: HAFAS (hafas=VBN) for Oldenburg(Oldb) Pulverturm, with a
// route that must not come back, and n departures more to test the bound.
func hafasPulverturm(more int) string {
	deps := []string{
		`{"train":"Bus 314","destination":"ZOB","scheduledTime":1795701120,"time":1795701720,"delay":10,"isCancelled":null,"platform":"B","route":[{"name":"Lappan"},{"name":"ZOB"}],"messages":[]}`,
		`{"train":"Bus 314","destination":"Wardenburg ZUH","scheduledTime":1795701360,"time":1795701780,"delay":7,"isCancelled":null,"platform":"A","route":[]}`,
		`{"train":"Bus 380","destination":"Oldenburg ZOB","scheduledTime":1795701600,"time":1795701780,"delay":3,"isCancelled":1,"platform":"B","route":[]}`,
		`{"train":"Bus 304","destination":"Ofenerdiek","scheduledTime":1795700400,"time":1795700400,"delay":0,"isCancelled":null,"platform":"B","route":[]}`,
	}
	for i := 0; i < more; i++ {
		deps = append(deps, fmt.Sprintf(`{"train":"Bus 322","destination":"Eversten-West","scheduledTime":%d,"time":%d,"delay":null,"isCancelled":null,"platform":"A"}`, 1795702000+60*i, 1795702000+60*i))
	}
	return `{"departures":[` + strings.Join(deps, ",") + `]}`
}

// FP-01 A HAFAS network's departures as a short list
func TestFP01_AHAFASNetworksDeparturesAsAShortList(t *testing.T) {
	f := &dbfFake{status: 200, body: hafasPulverturm(25)}
	run := runAbfahrten(t, f, map[string]any{"haltestelle": "Oldenburg(Oldb) Pulverturm", "verbund": "vbn"}, nil, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if len(f.asked) != 1 {
		t.Fatalf("asked %d times", len(f.asked))
	}
	u := f.asked[0]
	if u.EscapedPath() != "/Oldenburg(Oldb)%20Pulverturm.json" || u.Query().Get("hafas") != "VBN" || u.Query().Get("efa") != "" || u.Query().Get("limit") != "30" {
		t.Errorf("asked %s", u)
	}
	deps := departures(t, run.out)
	if len(deps) != 20 {
		t.Fatalf("%d departures, want at most 20", len(deps))
	}
	first := deps[0]
	if first["art"] != "Bus" || first["linie"] != "Bus 314" || first["ziel"] != "ZOB" || first["in_minuten"] != 12.0 ||
		first["uhrzeit"] != "14:52" || first["verspaetung"] != 10.0 || first["gleis"] != "B" {
		t.Errorf("first %v", first)
	}
	if _, ok := first["route"]; ok {
		t.Errorf("a route came back: %v", first)
	}
	if deps[2]["ausfall"] != true || deps[1]["ausfall"] != nil {
		t.Errorf("cancelled: %v / %v", deps[2], deps[1])
	}
	for _, d := range deps {
		if d["linie"] == "Bus 304" {
			t.Errorf("a departure already gone came back: %v", d)
		}
	}
	if run.out["netz"] != "VBN" || run.out["haltestelle"] != "Oldenburg(Oldb) Pulverturm" {
		t.Errorf("out %v", run.out)
	}
}

// FP-02 An EFA network's departures as a short list
func TestFP02_AnEFANetworksDeparturesAsAShortList(t *testing.T) {
	body := `{"departures":[
 {"countdown":"20","datetime":1795702200,"delay":null,"destination":"Durlach Turmberg","is_cancelled":0,"line":"1","platform_name":"Gleis 2","sched_datetime":1795702200,"rt_datetime":null,"type":"Straßenbahn","next_route":[{"name":"Marktplatz"}],"prev_route":[{"name":"Europaplatz"}]},
 {"countdown":"25","datetime":1795702620,"delay":3,"destination":"Rheinhafen","is_cancelled":1,"line":"5","platform_name":"","sched_datetime":1795702440,"rt_datetime":1795702620,"type":"Straßenbahn"}]}`
	f := &dbfFake{status: 200, body: body}
	run := runAbfahrten(t, f, map[string]any{"haltestelle": "Karlsruhe Marktplatz (Pyramide U)", "verbund": "KVV"}, nil, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if q := f.asked[0].Query(); q.Get("efa") != "KVV" || q.Get("hafas") != "" {
		t.Errorf("asked %s", f.asked[0])
	}
	deps := departures(t, run.out)
	if len(deps) != 2 {
		t.Fatalf("%v", deps)
	}
	if d := deps[0]; d["art"] != "Straßenbahn" || d["linie"] != "1" || d["ziel"] != "Durlach Turmberg" || d["in_minuten"] != 20.0 ||
		d["uhrzeit"] != "15:10" || d["gleis"] != "Gleis 2" || d["verspaetung"] != nil || d["next_route"] != nil {
		t.Errorf("first %v", d)
	}
	if d := deps[1]; d["in_minuten"] != 27.0 || d["uhrzeit"] != "15:14" || d["verspaetung"] != 3.0 || d["ausfall"] != true {
		t.Errorf("second %v", d)
	}
}

// FP-03 Trains without a network, with stops on the way and reasons
func TestFP03_TrainsWithoutANetworkWithStopsOnTheWayAndReasons(t *testing.T) {
	body := `{"departures":[
 {"train":"ICE 105","destination":"Karlsruhe Hbf","scheduledDeparture":null,"delayDeparture":null,"isCancelled":1,"platform":"2","via":[],"messages":{"delay":[{"text":"Defektes Stellwerk"}]}},
 {"train":"S S52","destination":"Karlsruhe Marktplatz","scheduledDeparture":"14:40","delayDeparture":0,"isCancelled":0,"platform":"21","via":["Poststraße"]},
 {"train":"RE 2","destination":"Mannheim Hbf","scheduledDeparture":"14:55","delayDeparture":5,"isCancelled":1,"platform":"3","via":["Durlach","Bruchsal","Wiesloch","Heidelberg Hbf","Mannheim-Friedrichsfeld"],"messages":{"delay":[{"text":"Defektes Stellwerk"},{"text":"Reparatur an der Strecke"},{"text":"Hohes Fahrgastaufkommen"}]}}]}`
	f := &dbfFake{status: 200, body: body}
	run := runAbfahrten(t, f, map[string]any{"haltestelle": "Karlsruhe Hbf"}, nil, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if q := f.asked[0].Query(); q.Get("efa") != "" || q.Get("hafas") != "" || q.Get("limit") != "30" {
		t.Errorf("asked %s", f.asked[0])
	}
	deps := departures(t, run.out)
	if len(deps) != 1 {
		t.Fatalf("want only RE 2 (ICE ends here, S52 is gone): %v", deps)
	}
	d := deps[0]
	ueber, _ := d["ueber"].([]any)
	hinweise, _ := d["hinweise"].([]any)
	if d["art"] != "RE" || d["linie"] != "RE 2" || d["uhrzeit"] != "14:55" || d["in_minuten"] != 10.0 || d["verspaetung"] != 5.0 ||
		d["ausfall"] != true || len(ueber) != 4 || ueber[3] != "Heidelberg Hbf" || len(hinweise) != 2 || hinweise[1] != "Hohes Fahrgastaufkommen" {
		t.Errorf("RE 2 %v", d)
	}
	if run.out["netz"] != "Bahn" {
		t.Errorf("netz %v", run.out["netz"])
	}
}

// FP-04 An unknown network is refused with the known ones
func TestFP04_AnUnknownNetworkIsRefusedWithTheKnownOnes(t *testing.T) {
	f := &dbfFake{status: 200, body: hafasPulverturm(0)}
	run := runAbfahrten(t, f, map[string]any{"haltestelle": "Pulverturm", "verbund": "VWG"}, nil, dbfNow)
	if run.err == nil || !strings.Contains(run.err.Error(), "„VWG“") || !strings.Contains(run.err.Error(), "VBN") ||
		!strings.Contains(run.err.Error(), "KVV") || !strings.Contains(run.err.Error(), "verbund") {
		t.Errorf("err %v", run.err)
	}
	if len(f.asked) != 0 {
		t.Errorf("dbf asked: %v", f.asked)
	}
}

// FP-05 A stop that is not found says how to write it
func TestFP05_AStopThatIsNotFoundSaysHowToWriteIt(t *testing.T) {
	for _, c := range []struct {
		name, verbund string
		status        int
		body, want    string
	}{
		{"efa, empty list", "KVV", 200, `{"departures":[]}`, "findet die Fahrplanauskunft nicht"},
		{"efa, name list", "KVV", 500, `{"api_version":"2.05","error":"Backend error: name list"}`, "nicht eindeutig"},
		{"efa, stop invalid", "KVV", 500, `{"api_version":"2.05","error":"Backend error: stop invalid"}`, "findet die Fahrplanauskunft nicht"},
		{"hafas, location", "VBN", 300, `{"api_version":"2.05","error":"svcResL[0].err is LOCATION"}`, "findet die Fahrplanauskunft nicht"},
		{"trains, ambiguous", "", 300, `{"api_version":"2.05","error":"Ambiguous station name"}`, "nicht eindeutig"},
	} {
		f := &dbfFake{status: c.status, body: c.body}
		run := runAbfahrten(t, f, map[string]any{"haltestelle": "Pulverturm", "verbund": c.verbund}, nil, dbfNow)
		if run.err == nil || !strings.Contains(run.err.Error(), "„Pulverturm“") || !strings.Contains(run.err.Error(), c.want) ||
			!strings.Contains(run.err.Error(), "so geschrieben sein, wie der Verbund ihn schreibt") {
			t.Errorf("%s: err %v", c.name, run.err)
		}
		if len(f.asked) != 1 {
			t.Errorf("%s: asked %d times", c.name, len(f.asked))
		}
	}
}

// FP-06 A direction filters the departures
func TestFP06_ADirectionFiltersTheDepartures(t *testing.T) {
	settings := map[string]any{"haltestelle": "Oldenburg(Oldb) Pulverturm", "verbund": "VBN"}
	f := &dbfFake{status: 200, body: hafasPulverturm(0)}
	run := runAbfahrten(t, f, settings, map[string]string{"richtung": "zob"}, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	deps := departures(t, run.out)
	if len(deps) != 2 || deps[0]["ziel"] != "ZOB" || deps[1]["ziel"] != "Oldenburg ZOB" || run.out["hinweis"] != nil {
		t.Errorf("zob: %v", run.out)
	}
	run = runAbfahrten(t, f, settings, map[string]string{"richtung": "Bremen"}, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if deps := departures(t, run.out); len(deps) != 0 || run.out["hinweis"] != "Keine der nächsten Abfahrten fährt Richtung Bremen." {
		t.Errorf("Bremen: %v", run.out)
	}
	// Another stop than the house's (decision 6): the network stays the setting's.
	run = runAbfahrten(t, f, settings, map[string]string{"haltestelle": "Oldenburg(Oldb) Lappan"}, dbfNow)
	if u := f.asked[len(f.asked)-1]; run.err != nil || u.EscapedPath() != "/Oldenburg(Oldb)%20Lappan.json" || u.Query().Get("hafas") != "VBN" {
		t.Errorf("another stop: %v %s", run.err, u)
	}
}

// FP-07 dbf failing is said, not retried
func TestFP07_DbfFailingIsSaidNotRetried(t *testing.T) {
	settings := map[string]any{"haltestelle": "Oldenburg(Oldb) Pulverturm", "verbund": "VBN"}
	for _, c := range []struct {
		name string
		f    *dbfFake
		want string
	}{
		{"503", &dbfFake{status: 503, body: "<html>Service Unavailable</html>"}, "HTTP 503: <html>Service Unavailable"},
		{"no JSON", &dbfFake{status: 200, body: "<html>Wartung</html>"}, "HTTP 200: <html>Wartung"},
		{"no answer", &dbfFake{err: errors.New("context deadline exceeded")}, "context deadline exceeded"},
	} {
		run := runAbfahrten(t, c.f, settings, nil, dbfNow)
		if run.err == nil || !strings.Contains(run.err.Error(), c.want) {
			t.Errorf("%s: err %v", c.name, run.err)
		}
		if len(c.f.asked) != 1 {
			t.Errorf("%s: asked %d times", c.name, len(c.f.asked))
		}
	}
}

// FP-08 A departure that does not read is left out
func TestFP08_ADepartureThatDoesNotReadIsLeftOut(t *testing.T) {
	body := `{"departures":[
 {"train":["Bus","314"],"destination":"ZOB","scheduledTime":1795701120,"time":1795701720},
 "kaputt",
 {"train":"Bus 314","destination":"Wardenburg ZUH","scheduledTime":1795701360,"time":"bald","delay":7}]}`
	f := &dbfFake{status: 200, body: body}
	run := runAbfahrten(t, f, map[string]any{"haltestelle": "Oldenburg(Oldb) Pulverturm", "verbund": "VBN"}, nil, dbfNow)
	if run.err != nil {
		t.Fatal(run.err)
	}
	deps := departures(t, run.out)
	if len(deps) != 1 || deps[0]["ziel"] != "Wardenburg ZUH" || deps[0]["in_minuten"] != 6.0 {
		t.Errorf("%v", deps)
	}
	var printed []string
	for _, e := range run.events {
		if e.Kind == "print" {
			printed = append(printed, e.Message)
		}
	}
	if len(printed) != 2 || !strings.Contains(printed[0], "left out") || !strings.Contains(printed[1], "not an object") {
		t.Errorf("printed %q", printed)
	}
	// All left out: the tool's error with the answer's start.
	f = &dbfFake{status: 200, body: `{"departures":["kaputt"]}`}
	run = runAbfahrten(t, f, map[string]any{"haltestelle": "Oldenburg(Oldb) Pulverturm", "verbund": "VBN"}, nil, dbfNow)
	if run.err == nil || !strings.Contains(run.err.Error(), "lässt sich nicht lesen") || !strings.Contains(run.err.Error(), "kaputt") {
		t.Errorf("all left out: %v", run.err)
	}
}

// FP-09 The skill uses the tool, not web_page
func TestFP09_TheSkillUsesTheToolNotWebPage(t *testing.T) {
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"fahrplan": {"haltestelle": "Karlsruhe Hbf"}})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("fahrplan did not load: %v", problems)
	}
	body := loaded[0].Body
	if !strings.Contains(body, "fahrplan__abfahrten") || strings.Contains(body, "dbf.finalrewind.org") || strings.Contains(body, "web_page") {
		t.Errorf("body:\n%s", body)
	}
}

// FP-10 The settings say how to write network and stop
func TestFP10_TheSettingsSayHowToWriteNetworkAndStop(t *testing.T) {
	loaded, _ := skills.Load([]string{"../../skills"}, map[string]map[string]any{"fahrplan": {"haltestelle": "Karlsruhe Hbf"}})
	if len(loaded) != 1 {
		t.Fatal("fahrplan did not load")
	}
	s := loaded[0].Settings
	if v := s["verbund"].Description; !strings.Contains(v, "VBN") || !strings.Contains(v, "KVV") {
		t.Errorf("verbund: %q", v)
	}
	if h := s["haltestelle"].Description; !strings.Contains(h, "so geschrieben, wie der Verkehrsverbund") || !strings.Contains(h, "Oldenburg(Oldb) Pulverturm") {
		t.Errorf("haltestelle: %q", h)
	}
}
