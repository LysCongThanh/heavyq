package redact

import "testing"

// TestCommandCredentialShapes covers credential forms beyond NAME=value and
// --flag value: quoted multi-word values, curl -u/--user, any Authorization
// scheme, mysql/sshpass/docker passwords, `set <key> <value>` and a URL
// password holding a raw '@'. All values are fake test fixtures.
func TestCommandCredentialShapes(t *testing.T) {
	cases := map[string]string{
		// quoted values hide every word, in a plain line and inside sh -c
		`PASSWORD="correct horse" ./run.sh`:               `PASSWORD=*** ./run.sh`,
		`deploy --password 'correct horse battery' --now`: `deploy --password *** --now`,
		`sh -c "PASSWORD='correct horse' ./run.sh"`:       `sh -c "PASSWORD=*** ./run.sh"`,
		`deploy --token="a b c" x`:                        `deploy --token=*** x`,
		// curl user:password
		"curl -u admin:hunter2secret https://api.example.com":      "curl -u admin:*** https://api.example.com",     // gitleaks:allow
		"curl --user admin:hunter2secret https://api.example.com":  "curl --user admin:*** https://api.example.com", // gitleaks:allow
		"curl --user=admin:hunter2secret https://api.example.com":  "curl --user=admin:*** https://api.example.com", // gitleaks:allow
		`curl -u "admin:correct horse" https://api.example.com`:    `curl -u admin:*** https://api.example.com`,     // gitleaks:allow
		"curl -H 'Authorization: token abcdef0123456789abcdef' u":  "curl -H 'Authorization: token ***' u",          // gitleaks:allow
		`curl -H "authorization: Digest abcdef0123456789abcdef" u`: `curl -H "authorization: Digest ***" u`,         // gitleaks:allow
		"curl -H 'Authorization: abcdef0123456789' u":              "curl -H 'Authorization: ***' u",                // gitleaks:allow
		// passwords of mysql-like tools, sshpass and docker login
		"mysql -u root -phunter2 app":                   "mysql -u root -p*** app",
		"mysqldump -h db -P 3306 -phunter2 app > d.sql": "mysqldump -h db -P 3306 -p*** app > d.sql",
		"docker login -p hunter2 -u me reg.example":     "docker login -p *** -u me reg.example",
		"sshpass -p hunter2 ssh -p 2222 host":           "sshpass -p *** ssh -p 2222 host",
		// a secret-looking key set to a value
		"aws configure set aws_secret_access_key wJalrXUtnFEMIexample": "aws configure set aws_secret_access_key ***", // gitleaks:allow
		"npm config set //registry.example/:_authToken abc123":         "npm config set //registry.example/:_authToken ***",
		// a raw '@' in a URL password: hide up to the last '@' before the host
		"psql postgresql://user:p@ss@db/x":       "psql postgresql://***@db/x",
		"git clone https://me:p@ss@host/r.git x": "git clone https://***@host/r.git x",
		// look-alikes that are not secrets stay readable
		"go test -p 4 ./internal/mysql/...":       "go test -p 4 ./internal/mysql/...",
		"mkdir -p build && ssh -p 2222 host true": "mkdir -p build && ssh -p 2222 host true",
		"docker run -u 1000:1000 img":             "docker run -u 1000:1000 img",
		"mysql -u root -p app":                    "mysql -u root -p app",
		"kubectl set image deploy/web web=img:1":  "kubectl set image deploy/web web=img:1",
		"curl https://example.com:8080/a?b=c@d":   "curl https://example.com:8080/a?b=c@d",
	}
	for in, want := range cases {
		if got := Command(in); got != want {
			t.Errorf("Command(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
