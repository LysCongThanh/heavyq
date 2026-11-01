package stance

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

const vietnameseBody = `Bạn đang tối ưu cách làm hiện tại. Hãy lùi lại một bước.

## Trước khi đọc mã
- Trả lời trước: "nếu làm lại từ đầu, giải pháp tốt nhất là gì?"
- Liệt kê ít nhất ba phương án, gồm hiện trạng và một phương án đột phá.

## Ràng buộc
- Phân loại mỗi ràng buộc: THẬT (bất biến, giới hạn cứng) hay QUÁN TÍNH (chỉ vì "xưa nay vẫn làm thế").
- Phá bỏ các ràng buộc quán tính.

Quyết định dựa trên giá trị đo được, kèm đường di chuyển.`

func builtinNamed(t *testing.T, name string) Stance {
	t.Helper()
	all, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := Find(all, name)
	if !ok {
		t.Fatalf("no built-in %q", name)
	}
	return s
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got+"\n" != string(want) {
		t.Fatalf("%s differs (run with -update after reviewing)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestStanceRenderGolden(t *testing.T) {
	viet := Stance{Name: "dot-pha", InjectTokens: 120, Body: vietnameseBody}
	cases := []struct {
		name string
		s    Stance
		why  string
	}{
		{"render_breakthrough", builtinNamed(t, "breakthrough"), "45 min, 11 runs, no proof on login-api"},
		{"render_skeptic", builtinNamed(t, "skeptic"), "claimed done, tree changed since the last proof"},
		{"render_vietnamese", viet, "45 phút, 11 lần chạy, chưa có bằng chứng"},
		{"render_tiny_budget", Stance{Name: "tiny", InjectTokens: MinInjectTokens, Body: vietnameseBody}, "đã chạy 11 lần mà chưa có bằng chứng nào cả, xin hãy dừng lại"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkGolden(t, c.name, Render(c.s, c.why)) })
	}
}

func TestStanceRenderStructure(t *testing.T) {
	s := builtinNamed(t, "breakthrough")
	out := Render(s, "stuck")
	lines := strings.Split(out, "\n")
	if lines[0] != "heavyq stance: breakthrough — why now: stuck" {
		t.Fatalf("header = %q", lines[0])
	}
	if last := lines[len(lines)-1]; last != "full text: heavyq stance show breakthrough" {
		t.Fatalf("footer = %q", last)
	}
	if len(lines) < 4 {
		t.Fatalf("expected body lines between header and footer:\n%s", out)
	}
}

// Whatever the budget, the body and the evidence: never over budget, always
// valid UTF-8, header first and pointer last.
func TestStanceRenderNeverExceedsBudget(t *testing.T) {
	bodies := map[string]string{
		"builtin":    builtinNamed(t, "breakthrough").Body,
		"vietnamese": vietnameseBody,
		"one-para":   strings.Repeat("Một đoạn văn rất dài không có xuống dòng. ", 200),
		"huge-list":  strings.Repeat("- mục danh sách khá dài để thử giới hạn ngân sách\n", 500),
		"one-line":   strings.Repeat("é", 3000),
		"empty":      "",
	}
	whys := []string{"", "short", strings.Repeat("rất lâu ", 300), "line one\nline two\n\n# injected heading", strings.Repeat("🙂", 400)}
	for bodyName, body := range bodies {
		for tokens := MinInjectTokens; tokens <= MaxInjectTokens; tokens += 25 {
			for _, why := range whys {
				s := Stance{Name: strings.Repeat("n", 40), InjectTokens: tokens, Body: body}
				out := Render(s, why)
				if len(out) > tokens*BytesPerToken {
					t.Fatalf("%s/%d tokens: %d bytes over budget %d", bodyName, tokens, len(out), tokens*BytesPerToken)
				}
				if !utf8.ValidString(out) {
					t.Fatalf("%s/%d tokens: invalid UTF-8", bodyName, tokens)
				}
				if !strings.HasPrefix(out, "heavyq stance: ") || !strings.HasSuffix(out, "heavyq stance show "+s.Name) {
					t.Fatalf("%s/%d tokens: header or footer lost:\n%s", bodyName, tokens, out)
				}
			}
		}
	}
}

func TestStanceRenderFlattensEvidenceToOneLine(t *testing.T) {
	out := Render(Stance{Name: "x", InjectTokens: 100, Body: "- a"}, "line one\nline two\n# heading")
	if first := strings.SplitN(out, "\n", 2)[0]; first != "heavyq stance: x — why now: line one line two # heading" {
		t.Fatalf("header = %q", first)
	}
}

func TestStanceRenderBudgetDefaultsAndCap(t *testing.T) {
	body := strings.Repeat("- item of the list that is fairly long\n", 400)
	if n := len(Render(Stance{Name: "x", Body: body}, "w")); n > DefaultInjectTokens*BytesPerToken || n < DefaultInjectTokens*BytesPerToken/2 {
		t.Fatalf("default budget: %d bytes", n)
	}
	if n := len(Render(Stance{Name: "x", InjectTokens: 100000, Body: body}, "w")); n > MaxInjectTokens*BytesPerToken {
		t.Fatalf("cap: %d bytes", n)
	}
}

func TestStanceRenderPrefersListItemsOverProseAndKeepsDocumentOrder(t *testing.T) {
	prose := strings.Repeat("prose ", 25) // 150 bytes: cannot fit next to the rest at the minimum budget
	body := "# Title\n" + prose + "\n- first\n- second\n"
	out := Render(Stance{Name: "x", InjectTokens: MinInjectTokens, Body: body}, "w")
	if strings.Contains(out, "prose") {
		t.Fatalf("prose must lose to list items and headings:\n%s", out)
	}
	if !strings.Contains(out, "# Title\n- first\n- second\n") {
		t.Fatalf("heading and items must be kept in document order:\n%s", out)
	}
}

func TestStanceRenderCutsAnOverlongFirstLine(t *testing.T) {
	out := Render(Stance{Name: "x", InjectTokens: MinInjectTokens, Body: strings.Repeat("ế", 500)}, "w")
	if !strings.Contains(out, "ế…\n") || len(out) > MinInjectTokens*BytesPerToken {
		t.Fatalf("want the first line cut with an ellipsis:\n%s", out)
	}
}
