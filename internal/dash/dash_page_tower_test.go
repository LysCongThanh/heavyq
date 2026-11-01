package dash

import (
	"regexp"
	"strings"
	"testing"
)

// maxPageBytes is the budget of the single-file page (issue #44).
const maxPageBytes = 70 * 1024

// The page is one HTML file with no external resources. These checks pin the
// Tower: the section exists, it has its empty state, every exception kind
// renders (the #demo data carries one of each), and dismissals are kept in
// localStorage behind try/catch (private modes throw).
func TestPageHasTower(t *testing.T) {
	f := start(t, false)
	code, page := f.do("GET", "/", "", nil)
	if code != 200 {
		t.Fatalf("page: %d", code)
	}
	if len(page) > maxPageBytes {
		t.Fatalf("page is %d bytes, budget %d", len(page), maxPageBytes)
	}
	for _, want := range []string{
		`class="tower"`, `Nothing needs you.`, `${tower(s)}${story(s)}`, // above the story
		`s.exceptions`, `data-act="snooze"`, `data-act="copy"`, `hq.snooze`, `SNOOZE_MS=600000`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, kind := range []string{"red_gate", "loop", "starved", "machine"} {
		if !strings.Contains(page, `kind:"`+kind+`"`) {
			t.Errorf("#demo data has no %s exception", kind)
		}
	}
	for _, act := range []string{"cancel", "raise", "run"} {
		if !strings.Contains(page, `action:{kind:"`+act+`"`) {
			t.Errorf("#demo data has no %s action", act)
		}
	}
	if !strings.Contains(page, "try{return localStorage.getItem(k)}catch") || strings.Count(page, "localStorage.") != 2 {
		t.Error("localStorage access must go through the store.get/store.set try/catch wrappers")
	}
	if strings.Contains(page, "http://") || strings.Contains(page, "https://") || regexp.MustCompile(`(src|href)="[^#"]`).MatchString(page) {
		t.Error("the page must not load external resources")
	}
}
