package cli

// The command table entry of `heavyq completion` and its dispatch; the handler is in completion_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "completion",
		Order:    230,
		Run:      func(inv *invocation) int { return completionCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "print a shell completion script (bash, zsh or fish)",
		Synopsis: []string{"heavyq completion bash|zsh|fish"},
		NextWord: []string{"bash", "zsh", "fish"},
		Notes: "bash:  source <(heavyq completion bash)         # or write it to a bash-completion directory\n" +
			"zsh:   heavyq completion zsh > \"${fpath[1]}/_heavyq\"\n" +
			"fish:  heavyq completion fish > ~/.config/fish/completions/heavyq.fish",
		Docs: "install.md",
	})
}
