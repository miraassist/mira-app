package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/companions"
)

// listedCompanions answers the page's list of companions; it passes nothing on.
type listedCompanions []companions.Companion

func (l listedCompanions) List(context.Context) ([]companions.Companion, error) { return l, nil }
func (listedCompanions) PassPage(w http.ResponseWriter, r *http.Request)        { http.NotFound(w, r) }
func (listedCompanions) PassData(w http.ResponseWriter, r *http.Request)        { http.NotFound(w, r) }

func TestBS24_MirasPageShowsTheSkillsPageAsATabInFaehigkeiten(t *testing.T) {
	h := New(Options{Token: "secret", Companions: listedCompanions{{Name: "pro", Title: "Pro", SkillsPage: "skills/"}, {Name: "plain", Title: "Plain"}}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	// The page learns of the skills page from the list of companions.
	code, body := asHA(t, srv, "GET", "/v1/ui/companions", "u-admin", true, "")
	if code != 200 || !strings.Contains(body, `{"name":"pro","title":"Pro","skills_page":"skills/"}`) ||
		!strings.Contains(body, `{"name":"plain","title":"Plain"}`) {
		t.Errorf("list %d %s", code, body)
	}
	// It makes a tab "<title> Skills" in the group "abilities" holding an
	// iframe of companion/<name>/<skills_page>, as the first page's tab in
	// "setup" holds companion/<name>/; it draws nothing of the page itself.
	resp, err := http.Get(srv.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(raw)
	for _, want := range []string{
		`abilities = GROUPS.find(g => g.id === "abilities")`,
		`add(setup, "companion-" + c.name, c.title);`,
		`add(abilities, "companion-" + c.name + ":skills", c.title + " Skills");`,
		`companionPages[c.name] = c.skills_page;`,
		`const page = part === "skills" ? companionPages[name] || "" : "";`,
		`src="${esc(url("/ui/companion/" + encodeURIComponent(name) + "/" + page))}"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
}

func TestBS26_TheTabSkillsIsGroupedByCategory(t *testing.T) {
	srv, _, _ := setup(t)
	get := func(path string) string {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return string(raw)
	}
	script, page := get("/ui/js/app.js"), get("/ui")
	for _, want := range []string{
		// Each state's skills by category: a heading per category, those without one last.
		`${this.byCategory(of("active"))}`, `${this.byCategory(of("problem"))}`, `${this.byCategory(of("available"))}`,
		`const cats = [...new Set(list.map(s => s.category || ""))].sort((a, b) => (a === "") - (b === "") || a.localeCompare(b));`,
		`<h3 class="skill-category">${c || T.cat_more}</h3>`,
		// No headings while no skill names a category.
		`if (!this.data.skills.some(x => x.category)) return`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the tab lacks %s", want)
		}
	}
	// "Weitere" in German, "More" in English, as the page's other texts.
	if !strings.Contains(page, `"cat_more": "Weitere",`) || !strings.Contains(page, `"cat_more": "More",`) {
		t.Error("the page lacks the label of the skills without a category in German or English")
	}
}
