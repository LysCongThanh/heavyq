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
		// '&', ';' and '|' in an attached -p value are part of the password
		{[]string{"mysql", "-u", "root", "-pS3cr&tPart2", "db"}, "mysql -u root -p*** db"}, // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-u", "root", "-pab;cd|ef", "db"}, "mysql -u root -p*** db"},    // gitleaks:allow (fake test fixture)
		// ';', '&' and '|' in an earlier argument (-e SQL) do not end the mysql command
		{[]string{"mysql", "-e", "USE app; SHOW TABLES", "-pS3cretpw", "db"}, "mysql -e USE app; SHOW TABLES -p*** db"},           // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-h", "db", "-e", "select 1;", "-pS3cretpw"}, "mysql -h db -e select 1; -p***"},                        // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-e", "a;b", "-pab cd", "db"}, "mysql -e a;b -p*** db"},                                                // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-e", "USE app; SHOW TABLES", "-pS3cr et", "db"}, "mysql -e USE app; SHOW TABLES -p*** db"},            // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", "mysql -e 'USE app; SHOW TABLES' -pS3cretpw db"}, "sh -c mysql -e 'USE app; SHOW TABLES' -p*** db"}, // gitleaks:allow (fake test fixture)
		{[]string{"curl", "-H", "Authorization: token abc def", "u"}, "curl -H Authorization: token *** u"},
		// no scheme: the raw value is hidden, the URL after it stays readable
		{[]string{"curl", "-H", "Authorization: abc123secretvalue", "https://api.example.com/x"}, "curl -H Authorization: *** https://api.example.com/x"},
		{[]string{"curl", "-H", "Authorization:abc123secretvalue", "https://x"}, "curl -H Authorization:*** https://x"},
		// a Bearer/Basic value outside an Authorization header
		{[]string{"curl", "-H", "X-Upstream: Bearer abcdefghijklmnop", "u"}, "curl -H X-Upstream: Bearer *** u"},
		{[]string{"myclient", "--auth-header", "Bearer abcdefghijklmnop"}, "myclient --auth-header Bearer ***"},
		{[]string{"curl", "-H", "X-Upstream: Basic dXNlcjpwYXNzd29yZA==", "u"}, "curl -H X-Upstream: Basic *** u"}, // gitleaks:allow (fake test fixture)
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
