package cli

// The command table entry of `heavyq version` and its dispatch; the handler is versionCommand in app.go.
func init() {
	registerCommand(commandSpec{
		Name:     "version",
		Order:    250,
		Aliases:  []string{"--version"},
		Run:      func(inv *invocation) int { return versionCommand(inv.Stdout) },
		Summary:  "print the version",
		Synopsis: []string{"heavyq version"},
	})
}
