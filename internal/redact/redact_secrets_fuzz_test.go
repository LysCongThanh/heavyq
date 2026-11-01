package redact

import "testing"

// maxFuzzInput bounds every fuzz input so one execution stays fast.
const maxFuzzInput = 64 << 10

// FuzzCommand checks redaction on arbitrary command lines: it never panics,
// is idempotent (redacting twice equals redacting once, so a stored line can
// be shown again safely) and grows the input by a bounded amount only.
//
// The smallest match of any pattern is 6 bytes ("auth=x") and a mask adds at
// most 2 bytes to it, so a third of the input plus a small constant bounds the
// growth.
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
		if limit := len(in) + len(in)/3 + 8; len(once) > limit {
			t.Fatalf("Command(%q) grew %d -> %d bytes (limit %d): %q", in, len(in), len(once), limit, once)
		}
	})
}
