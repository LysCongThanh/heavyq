package redact

import "testing"

// TestMysqlPasswordWithShellOperatorChars: in argv the shell has already
// removed the quotes, so '&', ';' and '|' inside an attached -pPASSWORD are
// part of the password, not the end of the mysql command. The whole value
// must be hidden and the result must stay a fixed point of Command. Values are
// fake test fixtures.
func TestMysqlPasswordWithShellOperatorChars(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"mysql", "-u", "root", "-pS3cr&tPart2", "db"}, "mysql -u root -p*** db"},   // gitleaks:allow (fake test fixture)
		{[]string{"mysql", "-u", "root", "-pab;cd|ef", "db"}, "mysql -u root -p*** db"},      // gitleaks:allow (fake test fixture)
		{[]string{"mysqldump", "-pa&b c;d", "-h", "db", "app"}, "mysqldump -p*** -h db app"}, // gitleaks:allow (fake test fixture)
		{[]string{"mariadb", "-u", "root", "-p|x&y", "db"}, "mariadb -u root -p*** db"},      // gitleaks:allow (fake test fixture)
		// a bare -p still prompts for the password
		{[]string{"mysql", "-u", "root", "-p", "db"}, "mysql -u root -p db"},
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
	// a plain line: an operator outside a -p value still ends the mysql stage,
	// so a later -p is not mysql's
	for in, want := range map[string]string{
		"mysql -u root -pS3cr&tPart2 db":     "mysql -u root -p*** db", // gitleaks:allow (fake test fixture)
		"mysql -u root -ph2 db | grep -p 4":  "mysql -u root -p*** db | grep -p 4",
		"mysql -u root db; mkdir -p x":       "mysql -u root db; mkdir -p x",
		"mysql -u root db && make -pq build": "mysql -u root db && make -pq build",
	} {
		if got := Command(in); got != want {
			t.Errorf("Command(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
