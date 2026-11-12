package queue

import "testing"

// TestTruncateCmdHidesWholeArgvValues: the shell has already removed the
// quotes, so a secret with spaces is one argv element. TruncateCmd must hide
// the whole element, not only its first word, while a sh -c script keeps the
// words that are not secret. Values are fake test fixtures.
func TestTruncateCmdHidesWholeArgvValues(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"deploy", "--password", "correct horse battery"}, "deploy --password ***"},
		{[]string{"deploy", "--token=correct horse", "--now"}, "deploy --token=*** --now"},
		{[]string{"env", "PASSWORD=correct horse", "./run.sh"}, "env PASSWORD=*** ./run.sh"},
		{[]string{"curl", "-u", "admin:correct horse", "https://api.example.com"}, "curl -u admin:*** https://api.example.com"},
		{[]string{"sshpass", "-p", "correct horse", "ssh", "host"}, "sshpass -p *** ssh host"},
		{[]string{"mysql", "-pcorrect horse", "app"}, "mysql -p*** app"},
		{[]string{"curl", "-H", "Authorization: token abc def", "u"}, "curl -H Authorization: token *** u"},
		// a shell script is shell text: only its secret value is hidden
		{[]string{"sh", "-c", "GITHUB_TOKEN=abc123 make release"}, "sh -c GITHUB_TOKEN=*** make release"},
		{[]string{"bash", "-lc", "PASSWORD='correct horse' ./run.sh"}, "bash -lc PASSWORD=*** ./run.sh"},
		// nothing secret: the display is unchanged
		{[]string{"sh", "-c", "go test ./..."}, "sh -c go test ./..."},
		{[]string{"mkdir", "-p", "my dir"}, "mkdir -p my dir"},
		{[]string{"git", "commit", "-m", "fix the login page"}, "git commit -m fix the login page"},
	}
	for _, c := range cases {
		if got := TruncateCmd(c.argv); got != c.want {
			t.Errorf("TruncateCmd(%q)\n got %q\nwant %q", c.argv, got, c.want)
		}
	}
}
