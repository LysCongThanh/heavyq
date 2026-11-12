package redact

import "testing"

// TestAuthorizationValueWithoutScheme: only a known auth scheme (Bearer,
// token, Digest, ...) is kept readable before the value. A raw value with no
// scheme is the value itself: it must be hidden, and the argument after it
// (a URL) must stay readable, whether the header is one argv element, a plain
// line or an unquoted sh -c script. Values are fake test fixtures.
func TestAuthorizationValueWithoutScheme(t *testing.T) {
	argvCases := []struct {
		argv []string
		want string
	}{
		{[]string{"curl", "-H", "Authorization: abc123secretvalue", "https://api.example.com/x"}, "curl -H Authorization: *** https://api.example.com/x"}, // gitleaks:allow (fake test fixture)
		{[]string{"curl", "-H", "Authorization:abc123secretvalue", "https://x"}, "curl -H Authorization:*** https://x"},                                   // gitleaks:allow (fake test fixture)
		{[]string{"curl", "-H", "authorization: abc123secretvalue"}, "curl -H authorization: ***"},                                                        // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", "curl -H Authorization: abc123secretvalue https://x"}, "sh -c curl -H Authorization: *** https://x"},                        // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", "curl -H Authorization:abc123secretvalue https://x"}, "sh -c curl -H Authorization:*** https://x"},                          // gitleaks:allow (fake test fixture)
		// a known scheme stays readable, the value after it is hidden
		{[]string{"curl", "-H", "Authorization: token abcdef0123456789", "https://x"}, "curl -H Authorization: token *** https://x"},
		{[]string{"curl", "-H", "Authorization: AWS4-HMAC-SHA256 Credential=abc/x", "https://x"}, "curl -H Authorization: AWS4-HMAC-SHA256 *** https://x"},
		{[]string{"curl", "-H", "Authorization: NTLM TlRMTVNTUAABAAAA", "https://x"}, "curl -H Authorization: NTLM *** https://x"},
		{[]string{"sh", "-c", "curl -H 'Authorization: Negotiate YIIabc' https://x"}, "sh -c curl -H 'Authorization: Negotiate ***' https://x"},
		// an unknown scheme in one argv element: the whole element is the value
		{[]string{"curl", "-H", "Authorization: ApiKey abc123secret", "https://x"}, "curl -H Authorization: *** https://x"},              // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", "curl -H 'Authorization: ApiKey abc123secret' https://x"}, "sh -c curl -H 'Authorization: ***' https://x"}, // gitleaks:allow (fake test fixture)
	}
	for _, c := range argvCases {
		got := Argv(c.argv)
		if got != c.want {
			t.Errorf("Argv(%q)\n got %q\nwant %q", c.argv, got, c.want)
		}
		if again := Command(got); again != got {
			t.Errorf("Argv(%q) = %q is not a fixed point of Command: %q", c.argv, got, again)
		}
	}
	lineCases := map[string]string{
		"curl -H Authorization: abc123secretvalue https://x":   "curl -H Authorization: *** https://x",          // gitleaks:allow (fake test fixture)
		"curl -H Authorization:abc123secretvalue https://x":    "curl -H Authorization:*** https://x",           // gitleaks:allow (fake test fixture)
		"curl -H 'Authorization: abc123secretvalue' https://x": "curl -H 'Authorization: ***' https://x",        // gitleaks:allow (fake test fixture)
		"curl -H 'Authorization: Mutual abc123' https://x":     "curl -H 'Authorization: Mutual ***' https://x", // gitleaks:allow (fake test fixture)
		// quoted: an unknown scheme or Digest parameters are hidden to the quote
		"curl -H 'Authorization: ApiKey abc123secret' https://x":               "curl -H 'Authorization: ***' https://x", // gitleaks:allow (fake test fixture)
		`curl -H "Authorization: Digest username=\"me\", response=\"ab12\"" u`: `curl -H "Authorization: Digest ***" u`,  // gitleaks:allow (fake test fixture)
		`curl -H 'Authorization: Digest username="me", response="ab12"' u`:     `curl -H 'Authorization: Digest ***' u`,  // gitleaks:allow (fake test fixture)
		"curl --header='Authorization: abc def ghi' u":                         "curl --header='Authorization: ***' u",
		"curl -H 'Authorization: Bearer abcdefghijklmnop":                      "curl -H 'Authorization: Bearer ***",
	}
	for in, want := range lineCases {
		if got := Command(in); got != want {
			t.Errorf("Command(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
