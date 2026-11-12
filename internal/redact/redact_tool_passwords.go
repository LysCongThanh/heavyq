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
	// -pPASSWORD is taken whole first: in argv the shell already removed the
	// quotes, so a '&', ';' or '|' in it is part of the password, not the end
	// of the stage
	mysqlLine = regexp.MustCompile(`\b(?:mysql[a-z]*|mariadb[a-z-]*)\b(?:` + sep + `-p` + word + `|[^|;&\n])*`)
	// its -pPASSWORD: attached, as a bare -p prompts and -p db names a database
	mysqlPassword = regexp.MustCompile(`(` + sep + `-p)` + word)
)

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
