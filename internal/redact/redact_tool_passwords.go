package redact

import (
	"regexp"
	"strings"
)

// Password shapes that a plain pattern cannot judge alone.
var (
	// -u user:password / --user user:password (curl, wget, httpie...);
	// maskUserPassword decides from the value
	userPassword = regexp.MustCompile(`(^|` + sep + `)(-u|--user|--proxy-user)(=|` + sep + `*)(` + word + `)`)
	// a mysql-family command up to the end of its pipeline stage. An attached
	// -pPASSWORD and a quoted string are taken whole first: a '&', ';' or '|'
	// in them is text, not the end of the stage (in argv the shell already
	// removed the quotes of the password; quoted SQL keeps its own)
	mysqlLine = regexp.MustCompile(`\b` + mysqlWord + `\b(?:` + sep + `-p` + word + `|` + quoted + `|[^|;&\n"'])*`)
	// its -pPASSWORD: attached, as a bare -p prompts and -p db names a database
	mysqlPassword = regexp.MustCompile(`(` + sep + `-p)` + word)
	// an argv element that names a mysql-family command (mysql, /usr/bin/mysqldump)
	mysqlArg = regexp.MustCompile(`\b` + mysqlWord + `\b`)
)

const mysqlWord = `(?:mysql[a-z]*|mariadb[a-z-]*)`

// maskMysqlArgvPasswords hides every attached -pPASSWORD element after an
// element naming a mysql-family command, in place. Argv has no shell
// operators, so the mysql command runs to the end of the argv: a ';', '&' or
// '|' in an earlier element (multi-statement -e SQL) is argument text that
// Command, reading the joined line, would take for the end of the stage.
// A bare -p prompts for the password and stays.
func maskMysqlArgvPasswords(parts []string) {
	inMysql := false
	for i, a := range parts {
		if inMysql && len(a) > 2 && strings.HasPrefix(a, "-p") {
			parts[i] = "-p***"
		}
		inMysql = inMysql || mysqlArg.MatchString(a)
	}
}

// maskUserPassword hides the password of a user:password value, quoted or
// not. A value without a colon (mysql -u root) and a numeric uid:gid
// (docker run -u 1000:1000) are not secrets and stay as they are.
func maskUserPassword(m string) string {
	g := userPassword.FindStringSubmatch(m)
	if g == nil {
		return m
	}
	v := g[4]
	if q := v[0]; q == '"' || q == '\'' {
		v = strings.TrimSuffix(v[1:], string(q))
	}
	user, pass, ok := strings.Cut(v, ":")
	if !ok || pass == "" || (digits(user) && digits(pass)) {
		return m
	}
	if strings.ContainsAny(user, `"'`) { // shown, its quote would swallow what follows
		return g[1] + g[2] + g[3] + "***"
	}
	return g[1] + g[2] + g[3] + user + ":***"
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
