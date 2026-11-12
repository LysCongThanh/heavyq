package redact

import "testing"

// TestArgvHidesBearerAndBasicValues: Argv stands NUL in for the spaces inside
// an argv element, so a Bearer/Basic value in a header that is not
// Authorization, or in a flag value, must still be hidden, and the result
// must stay a fixed point of Command. Values are fake test fixtures.
func TestArgvHidesBearerAndBasicValues(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"curl", "-H", "X-Upstream: Bearer abcdefghijklmnop", "u"}, "curl -H X-Upstream: Bearer *** u"},
		{[]string{"myclient", "--auth-header", "Bearer abcdefghijklmnop"}, "myclient --auth-header Bearer ***"},
		{[]string{"curl", "-H", "X-Upstream: Basic dXNlcjpwYXNzd29yZA==", "u"}, "curl -H X-Upstream: Basic *** u"}, // gitleaks:allow
		{[]string{"sh", "-c", "curl -H 'X-Upstream: Bearer abcdefghijklmnop' u"}, "sh -c curl -H 'X-Upstream: Bearer ***' u"},
	}
	for _, c := range cases {
		got := Argv(c.argv)
		if got != c.want {
			t.Errorf("Argv(%q)\n got %q\nwant %q", c.argv, got, c.want)
		}
		if again := Command(got); again != got {
			t.Errorf("Argv(%q) = %q is not a fixed point of Command: %q", c.argv, got, again)
		}
	}
}
