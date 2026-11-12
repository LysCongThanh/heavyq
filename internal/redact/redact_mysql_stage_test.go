package redact

import "testing"

// TestMysqlPasswordAfterOperatorCharsInEarlierArgs: in argv the shell has
// already removed the quotes and there are no shell operators, so a ';', '&'
// or '|' in an earlier argument (multi-statement -e SQL) is argument text and
// must not end the mysql command before its -pPASSWORD. In a shell script a
// quoted ';' does not end it either. Values are fake test fixtures.
func TestMysqlPasswordAfterOperatorCharsInEarlierArgs(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"mysql", "-e", "USE app; SHOW TABLES", "-pS3cretpw", "db"}, "mysql -e USE app; SHOW TABLES -p*** db"},                     // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "--execute=SELECT 1; SELECT 2", "-u", "root", "-pS3cretpw"}, "mysql --execute=SELECT 1; SELECT 2 -u root -p***"}, // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-h", "db", "-e", "select 1;", "-pS3cretpw"}, "mysql -h db -e select 1; -p***"},                                  // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-e", "select;", "-pS3cretpw"}, "mysql -e select; -p***"},                                                        // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-e", "select a|b from t", "-pS3cretpw"}, "mysql -e select a|b from t -p***"},                                    // gitleaks:allow (fake test fixture)
		{[]string{"docker", "exec", "c", "mysql", "-e", "a&b", "-pS3cretpw"}, "docker exec c mysql -e a&b -p***"},                           // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", "mysql -e 'USE app; SHOW TABLES' -pS3cretpw db"}, "sh -c mysql -e 'USE app; SHOW TABLES' -p*** db"},           // gitleaks:allow (fake test fixture)
		{[]string{"sh", "-c", `mysql -e "a|b" -pS3cretpw db`}, `sh -c mysql -e "a|b" -p*** db`},                                             // gitleaks:allow (fake test fixture)
		// a bare -p still prompts; a -p before the mysql word is not mysql's
		{[]string{"mysql", "-e", "a;b", "-p", "db"}, "mysql -e a;b -p db"},
		{[]string{"make", "-pq", "mysql"}, "make -pq mysql"},
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
	// a plain line: a quoted operator stays inside the mysql stage, an
	// unquoted one still ends it
	for in, want := range map[string]string{
		"mysql -e 'USE app; SHOW TABLES' -pS3cretpw db": "mysql -e 'USE app; SHOW TABLES' -p*** db", // gitleaks:allow (fake test fixture)
		`mysql -e "x & y" -pS3cretpw`:                   `mysql -e "x & y" -p***`,                   // gitleaks:allow (fake test fixture)
		"mysql -e 'x' db; mkdir -p y":                   "mysql -e 'x' db; mkdir -p y",
	} {
		if got := Command(in); got != want {
			t.Errorf("Command(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
