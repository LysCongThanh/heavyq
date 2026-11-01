package redact

import "testing"

func TestCommand(t *testing.T) {
	cases := map[string]string{
		"go test ./...": "go test ./...",
		"DATABASE_URL=postgres://app:s3cr3t@localhost/app_wt3 go test ./...": "DATABASE_URL=postgres://***@localhost/app_wt3 go test ./...",
		"GITHUB_TOKEN=abc123 make release":                                   "GITHUB_TOKEN=*** make release",
		"mysql --password=hunter2 -e 'select 1'":                             "mysql --password=*** -e 'select 1'",
		"curl -H 'Authorization: Bearer eyJhbGciOiJIUzI1NiJ9' x":             "curl -H 'Authorization: Bearer ***' x", // gitleaks:allow (fake test fixture)
		"deploy --token ghp_0123456789abcdefghijABCDEFGHIJ":                  "deploy --token ***",                    // gitleaks:allow
		"OPENAI_API_KEY=sk-proj-aaaaaaaaaaaaaaaaaaaaaaaa npm test":           "OPENAI_API_KEY=*** npm test",           // gitleaks:allow
		"npm test -- --grep auth":                                            "npm test -- --grep auth",
		"curl -H 'X-Api-Key: abcdef123' u":                                   "curl -H 'X-Api-Key: ***' u", // gitleaks:allow (fake test fixture)
		"pytest tests/test_auth.py":                                          "pytest tests/test_auth.py",
	}
	for in, want := range cases {
		if got := Command(in); got != want {
			t.Errorf("Command(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
