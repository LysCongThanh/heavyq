package cli

import "sort"

// compWord is one completion candidate with its description.
type compWord struct{ Text, Desc string }

// compRow says which words are offered when the line so far has command Cmd
// and subcommand Sub. Sub "*" means any (or no) second word, for commands
// without subcommands. Cmd "" is the first word. "run" is the <prio 1-9> form.
type compRow struct {
	Cmd, Sub string
	Words    []compWord
}

// completionRows derives every row from the command table.
func completionRows() []compRow {
	top := []compWord{}
	for _, c := range commandTable() {
		if c.Name != "run" {
			top = append(top, compWord{c.Name, c.Summary})
		}
	}
	top = append(top,
		compWord{"--config", configFlag.Help}, compWord{"--help", "show the usage"}, compWord{"--version", "print the version"})
	rows := []compRow{{Cmd: "", Sub: "*", Words: top}}
	for _, c := range commandTable() {
		if len(c.Subs) == 0 {
			words := nextWords(c)
			words = append(words, flagWords(c.Flags)...)
			rows = append(rows, compRow{Cmd: c.Name, Sub: "*", Words: words})
			continue
		}
		subs := make([]compWord, 0, len(c.Subs))
		for _, s := range c.Subs {
			subs = append(subs, compWord{s.Name, s.Summary})
		}
		rows = append(rows, compRow{Cmd: c.Name, Sub: "", Words: append(subs, flagWords(c.Flags)...)})
		for _, s := range c.Subs {
			words := make([]compWord, 0, len(s.NextWord))
			for _, n := range s.NextWord {
				words = append(words, compWord{Text: n})
			}
			words = append(words, flagWords(s.Flags)...)
			words = append(words, flagWords(c.Flags)...)
			rows = append(rows, compRow{Cmd: c.Name, Sub: s.Name, Words: words})
		}
	}
	return rows
}

// nextWords are the fixed words that may follow a command without subcommands.
func nextWords(c commandSpec) []compWord {
	var out []compWord
	for _, n := range c.NextWord {
		out = append(out, compWord{Text: n})
	}
	if c.NextCommands {
		for _, n := range commandNames() {
			out = append(out, compWord{Text: n})
		}
	}
	return out
}

// flagWords lists every spelling of the flags, with their help.
func flagWords(flags []flagSpec) []compWord {
	var out []compWord
	for _, f := range flags {
		for _, w := range f.words() {
			out = append(out, compWord{w, f.Help})
		}
	}
	return out
}

// valueFlag is a flag that takes a value, with what may complete it.
type valueFlag struct {
	Word    string   // "--scope"
	Choices []string // fixed candidates
	Path    string   // "file" or "dir" when the value is a path
}

// valueFlags lists the flags that take a value, one per spelling and sorted,
// so a completer can stop offering command words right after them.
func valueFlags() []valueFlag {
	seen := map[string]valueFlag{}
	add := func(f flagSpec) {
		if f.Value == "" {
			return
		}
		for _, w := range f.words() {
			if _, dup := seen[w]; !dup {
				seen[w] = valueFlag{Word: w, Choices: f.Choices, Path: f.Path}
			}
		}
	}
	add(configFlag)
	for _, c := range commandTable() {
		for _, f := range c.Flags {
			add(f)
		}
		for _, s := range c.Subs {
			for _, f := range s.Flags {
				add(f)
			}
		}
	}
	out := make([]valueFlag, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Word < out[j].Word })
	return out
}
