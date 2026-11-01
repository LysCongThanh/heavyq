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

<!-- core -->
- Trả lời trước: "nếu làm lại từ đầu, giải pháp tốt nhất là gì?"
- Liệt kê ít nhất ba phương án, gồm một phương án đột phá.
- Phá bỏ các ràng buộc quán tính.
<!-- /core -->

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
	viet := Stance{Name: "dot-pha", Next: "viết câu trả lời từ đầu trước khi đọc thiết kế hiện tại", Body: vietnameseBody}
	cases := []struct {
		name string
		s    Stance
		why  string
	}{
		{"render_breakthrough", builtinNamed(t, "breakthrough"), "same failure class on 3 trees: TestLogin timeout"},
		{"render_skeptic", builtinNamed(t, "skeptic"), "claimed done, tree changed since the last proof"},
		{"render_vietnamese", viet, "cùng một lỗi trên 3 cây mã khác nhau: TestDangNhap"},
		{"render_no_core_block", Stance{Name: "plain", Body: "# T\nintro\n- one\n- two\n- three\n- four\n"}, "why"},
		{"render_tiny_budget", Stance{Name: "tiny", InjectTokens: MinInjectTokens, Next: viet.Next, Body: vietnameseBody}, "đã chạy 11 lần mà chưa có bằng chứng nào cả, xin hãy dừng lại và nhìn lại toàn bộ"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkGolden(t, c.name, Render(c.s, c.why)) })
	}
}

func TestStanceRenderFactsFirstStructure(t *testing.T) {
	s := builtinNamed(t, "breakthrough")
	lines := strings.Split(Render(s, "stuck"), "\n")
	if lines[0] != "[heavyq · stance breakthrough] why now: stuck" {
		t.Fatalf("line 1 = %q", lines[0])
	}
	if lines[1] != "next: "+s.Next {
		t.Fatalf("line 2 = %q", lines[1])
	}
	if last := lines[len(lines)-1]; last != "full: heavyq stance show breakthrough" {
		t.Fatalf("footer = %q", last)
	}
	if core := lines[2 : len(lines)-1]; len(core) != 3 || !strings.HasPrefix(core[0], "- Answer") {
		t.Fatalf("core lines = %q", core)
	}
}

func TestStanceRenderOmitsNextWhenUnset(t *testing.T) {
	lines := strings.Split(Render(Stance{Name: "x", Body: "- a\n- b"}, "w"), "\n")
	if len(lines) != 4 || lines[0] != "[heavyq · stance x] why now: w" || lines[1] != "- a" || lines[3] != "full: heavyq stance show x" {
		t.Fatalf("%q", lines)
	}
}

// Core blocks win over everything else, at most three lines are injected,
// and the markers themselves never are.
func TestStanceRenderCoreBlocksAndLineCap(t *testing.T) {
	body := "- early item\n<!-- core -->\nfirst core\n\n- second core\n<!-- /core -->\nprose\n<!-- core -->\nthird core\nfourth core\n<!-- /core -->\n"
	out := Render(Stance{Name: "x", Body: body}, "w")
	want := "[heavyq · stance x] why now: w\nfirst core\n- second core\nthird core\nfull: heavyq stance show x"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestStanceRenderFallsBackToProseWithoutItems(t *testing.T) {
	out := Render(Stance{Name: "x", Body: "# Heading\nOne sentence.\nAnother one.\n"}, "w")
	if !strings.Contains(out, "\nOne sentence.\nAnother one.\n") || strings.Contains(out, "Heading") {
		t.Fatalf("%s", out)
	}
}

// Whatever the budget, the body and the evidence: never over budget, always
// valid UTF-8, facts first and pointer last.
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
	nexts := []string{"", "run: heavyq gate status", strings.Repeat("hãy chạy lại ", 100)}
	for bodyName, body := range bodies {
		for tokens := MinInjectTokens; tokens <= MaxInjectTokens; tokens += 20 {
			for _, why := range whys {
				for _, next := range nexts {
					s := Stance{Name: strings.Repeat("n", 40), InjectTokens: tokens, Next: next, Body: body}
					out := Render(s, why)
					if len(out) > tokens*BytesPerToken {
						t.Fatalf("%s/%d tokens: %d bytes over budget %d", bodyName, tokens, len(out), tokens*BytesPerToken)
					}
					if !utf8.ValidString(out) {
						t.Fatalf("%s/%d tokens: invalid UTF-8", bodyName, tokens)
					}
					if !strings.HasPrefix(out, "[heavyq · stance ") || !strings.HasSuffix(out, "heavyq stance show "+s.Name) {
						t.Fatalf("%s/%d tokens: header or footer lost:\n%s", bodyName, tokens, out)
					}
				}
			}
		}
	}
}

func TestStanceRenderFlattensEvidenceToOneLine(t *testing.T) {
	out := Render(Stance{Name: "x", InjectTokens: 100, Body: "- a"}, "line one\nline two\n# heading")
	if first := strings.SplitN(out, "\n", 2)[0]; first != "[heavyq · stance x] why now: line one line two # heading" {
		t.Fatalf("header = %q", first)
	}
}

func TestStanceRenderBudgetDefaultsAndCap(t *testing.T) {
	body := strings.Repeat("- item of the list that is fairly long\n", 400)
	if a, b := Render(Stance{Name: "x", Body: body}, "w"), Render(Stance{Name: "x", InjectTokens: DefaultInjectTokens, Body: body}, "w"); a != b {
		t.Fatalf("unset inject_tokens must mean the default:\n%s\n%s", a, b)
	}
	long := strings.Repeat("w ", 3000)
	if n := len(Render(Stance{Name: "x", InjectTokens: 100000, Body: body}, long)); n > MaxInjectTokens*BytesPerToken {
		t.Fatalf("cap: %d bytes", n)
	}
}

func TestStanceRenderSkipsLinesThatDoNotFitAndCutsAnOverlongFirstLine(t *testing.T) {
	long := strings.Repeat("long ", 80)
	out := Render(Stance{Name: "x", InjectTokens: MinInjectTokens, Body: "- " + long + "\n- short"}, "w")
	if !strings.Contains(out, "\n- short\n") || strings.Contains(out, "long") {
		t.Fatalf("an overlong line must be skipped, a short one kept:\n%s", out)
	}
	out = Render(Stance{Name: "x", InjectTokens: MinInjectTokens, Body: strings.Repeat("ế", 500)}, "w")
	if !strings.Contains(out, "ế…\n") || len(out) > MinInjectTokens*BytesPerToken {
		t.Fatalf("want the first line cut with an ellipsis:\n%s", out)
	}
}
