package redact

import (
	"strings"
	"testing"
)

// maxFuzzInput bounds every fuzz input so one execution stays fast.
const maxFuzzInput = 64 << 10

// FuzzCommand checks redaction on arbitrary command lines: it never panics,
// is idempotent (redacting twice equals redacting once, so a stored line can
// be shown again safely) and grows the input by a bounded amount only.
//
// A mask replaces at least one byte with "***", so it adds at most 2 bytes,
// and the smallest match is 4 bytes (" -px" in a mysql line), so half of the
// input plus a small constant bounds the growth.
func FuzzCommand(f *testing.F) {
	for _, s := range []string{
		"go test ./...",
		"DATABASE_URL=postgres://app:s3cr3t@localhost/app_wt3 go test ./...",
		"GITHUB_TOKEN=abc123 make release",
		"mysql --password=hunter2 -e 'select 1'",
		"curl -H 'Authorization: Bearer eyJhbGciOiJIUzI1NiJ9' x",   // gitleaks:allow (fake test fixture)
		"deploy --token ghp_0123456789abcdefghijABCDEFGHIJ",        // gitleaks:allow
		"OPENAI_API_KEY=sk-proj-aaaaaaaaaaaaaaaaaaaaaaaa npm test", // gitleaks:allow
		"npm test -- --grep auth",
		"curl -H 'X-Api-Key: abcdef123' u", // gitleaks:allow (fake test fixture)
		"pytest tests/test_auth.py",
		"auth=x token:y -auth=z a://b:c@d bearer 12345678", // shortest matches of each pattern
		"--token --token --token=a -token b",
		"\xff\xfe secret=\x00",
		`PASSWORD="a b" sh -c "TOKEN='c d' x" -u "u:p w"`,
		"curl -u a:b --user=c:d -H 'Authorization: token e' postgresql://f:g@h@i/j",
		"mysql -pa -pb | sshpass -p c ssh -p 2 x; docker login -p d; aws configure set aws_secret_access_key e",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if len(in) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		once := Command(in)
		if twice := Command(once); twice != once {
			t.Fatalf("not idempotent for %q\n once  %q\n twice %q", in, once, twice)
		}
		if limit := len(in) + len(in)/2 + 8; len(once) > limit {
			t.Fatalf("Command(%q) grew %d -> %d bytes (limit %d): %q", in, len(in), len(once), limit, once)
		}
	})
}

// FuzzArgv checks the argv form on arbitrary argv (elements split at \x1f):
// the joined result is a fixed point of Command (so a stored line can be
// redacted again when shown) and never carries the NUL Argv uses inside.
func FuzzArgv(f *testing.F) {
	for _, s := range []string{
		"deploy\x1f--password\x1fcorrect horse battery",
		"env\x1fPASSWORD=a b\x1fsh\x1f-c\x1fTOKEN='c d' make x",
		"curl\x1f-u\x1fadmin:a b\x1f-H\x1fAuthorization: token c d",
		"mysql\x1f-pa b\x1fsshpass\x1f-p\x1fc d",
		"a b\x1f\x00c d",
		"curl\x1f-H\x1fX-Upstream: Bearer abcdefghijklmnop\x1fu",
		"myclient\x1f--auth-header\x1fBearer abcdefghijklmnop",
		"curl\x1f-H\x1fX-Upstream: Basic dXNlcjpwYXNzd29yZA==\x1fu", // gitleaks:allow (fake test fixture)
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if len(in) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		argv := strings.Split(in, "\x1f")
		got := Argv(argv)
		if again := Command(got); again != got {
			t.Fatalf("Argv(%q) is not a fixed point of Command\n argv  %q\n again %q", argv, got, again)
		}
		if !strings.Contains(in, "\x00") && strings.Contains(got, "\x00") {
			t.Fatalf("Argv(%q) leaks its NUL stand-in: %q", argv, got)
		}
	})
}
