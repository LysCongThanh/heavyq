// Package stance holds "stances": short documents that describe a way of
// thinking (breakthrough, converge, skeptic, ...). This package is the pure
// part: the file format, the loader (built-in, user, repo) and the compact
// renderer. It runs nothing and reads no ledger; triggers and injection live
// elsewhere and build on it.
package stance

// Source says where a stance came from. Precedence, highest first: repo,
// user, builtin.
type Source string

const (
	SourceRepo    Source = "repo"
	SourceUser    Source = "user"
	SourceBuiltin Source = "builtin"
)

// Limits, all in bytes unless noted. They bound what a stance file can cost
// to read and inject; anything bigger is an error, never truncated.
const (
	MaxBodyBytes        = 32 << 10
	maxFrontmatterBytes = 8 << 10
	maxDescriptionRunes = 160
	maxNextRunes        = 160
	maxKeywords         = 20
	maxKeywordRunes     = 80

	DefaultInjectTokens = 150
	MinInjectTokens     = 80
	MaxInjectTokens     = 400
	// BytesPerToken is the (deliberately rough) conversion the injection
	// budget uses: inject_tokens x 4 bytes.
	BytesPerToken = 4
)

// Stance is one thinking mode. The first group of fields is the frontmatter
// (TOML keys); Body, Source and Path are filled by the loader.
type Stance struct {
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description"`
	// Next is the one command or check the injection leads with after the
	// evidence ("next: ..."). Optional: without it the line is omitted.
	Next            string `toml:"next" json:"next,omitempty"`
	Enter           Enter  `toml:"enter" json:"enter"`
	Exit            Exit   `toml:"exit" json:"exit"`
	CooldownMinutes int    `toml:"cooldown_minutes" json:"cooldown_minutes"`
	InjectTokens    int    `toml:"inject_tokens" json:"inject_tokens"`

	Body   string `toml:"-" json:"body"`
	Source Source `toml:"-" json:"source"`
	// Path is the file the stance was read from, or "builtin:<name>".
	Path string `toml:"-" json:"path"`
}

// Enter lists the evidence that should switch a lane into the stance. Zero
// values mean "this trigger is off". Evaluating them is not this package's
// job; it only validates and carries them.
type Enter struct {
	Stall                       *Stall   `toml:"stall" json:"stall,omitempty"`
	SameFailureClassAcrossTrees int      `toml:"same_failure_class_across_trees" json:"same_failure_class_across_trees,omitempty"`
	ChurnReverts                int      `toml:"churn_reverts" json:"churn_reverts,omitempty"`
	Keywords                    []string `toml:"keywords" json:"keywords,omitempty"`
}

// Stall is "claimed for Minutes with at least Runs runs, and (when
// WithoutProof) no proof". Time-only evidence is not a reason to interrupt an
// agent, so the key stays parseable for status lines and dashboards but no
// built-in stance uses it to enter, and triggers must not act on it alone.
type Stall struct {
	Minutes      int  `toml:"minutes" json:"minutes"`
	Runs         int  `toml:"runs" json:"runs"`
	WithoutProof bool `toml:"without_proof" json:"without_proof"`
}

// Exit says when the stance is over.
type Exit struct {
	DecisionRecord bool `toml:"decision_record" json:"decision_record,omitempty"`
	TimeboxMinutes int  `toml:"timebox_minutes" json:"timebox_minutes,omitempty"`
}
