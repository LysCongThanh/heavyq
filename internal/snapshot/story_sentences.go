package snapshot

import (
	"strings"
	"time"
)

// Sentence is one sentence of the story: the state of the queue in plain
// words. Text is the same sentence without markup (terminal, logs, agent
// briefs); Parts carry the emphasis for renderers that can show it.
type Sentence struct {
	Text  string `json:"text"`
	Parts []Span `json:"parts"`
}

// Span is a run of text inside a sentence. Concatenating the Text of all
// spans gives Sentence.Text. False and empty fields are omitted from JSON.
type Span struct {
	Text string `json:"text"`
	Em   bool   `json:"em,omitempty"`   // emphasised: the subject or a key value
	Num  bool   `json:"num,omitempty"`  // a figure (with Em): render in tabular numerals
	Lane string `json:"lane,omitempty"` // a lane name: render as the lane's coloured chip
	Hot  bool   `json:"hot,omitempty"`  // a problem: render in the warning colour
}

// Story explains the queue to a human or an agent: why jobs wait, what holds
// the RAM, when the anti-starvation bypass applies. It is the one source for
// the terminal (`heavyq status`, `heavyq why`), the dashboard and
// `snapshot.story`, so they all say the same thing.
//
// now is the time the sentences are read at: countdowns are measured from
// s.GeneratedAt. ins is optional; with it, a last sentence summarises the
// window it covers. Story never returns an empty list.
func Story(s Snapshot, ins *Insights, now time.Time) []Sentence {
	elapsed := elapsedSince(s, now)
	var head *Waiting
	if len(s.Waiting) > 0 {
		head = &s.Waiting[0]
	}
	out := []Sentence{queueSentence(s, head)}
	for _, x := range []*Sentence{
		outsideSentence(s),
		bypassSentence(s, head, elapsed),
		forcedSentence(s),
		unreachableSentence(s),
		windowSentence(ins),
	} {
		if x != nil {
			out = append(out, *x)
		}
	}
	return out
}

// elapsedSince is how many seconds passed between the snapshot and now
// (0 for a snapshot without a time, or a clock that went backwards).
func elapsedSince(s Snapshot, now time.Time) float64 {
	if s.GeneratedAt.IsZero() {
		return 0
	}
	return max(0, now.Sub(s.GeneratedAt).Seconds())
}

// sentenceBuilder assembles the spans of one sentence.
type sentenceBuilder struct{ parts []Span }

func (b *sentenceBuilder) text(t string) *sentenceBuilder {
	b.parts = append(b.parts, Span{Text: t})
	return b
}

func (b *sentenceBuilder) em(t string) *sentenceBuilder {
	b.parts = append(b.parts, Span{Text: t, Em: true})
	return b
}

func (b *sentenceBuilder) num(t string) *sentenceBuilder {
	b.parts = append(b.parts, Span{Text: t, Em: true, Num: true})
	return b
}

func (b *sentenceBuilder) lane(l string) *sentenceBuilder {
	b.parts = append(b.parts, Span{Text: l, Lane: l})
	return b
}

func (b *sentenceBuilder) hot(t string) *sentenceBuilder {
	b.parts = append(b.parts, Span{Text: t, Hot: true})
	return b
}

func (b *sentenceBuilder) spans(p ...Span) *sentenceBuilder {
	b.parts = append(b.parts, p...)
	return b
}

// build returns the sentence; its Text is the spans joined.
func (b *sentenceBuilder) build() *Sentence {
	var t strings.Builder
	for _, p := range b.parts {
		t.WriteString(p.Text)
	}
	return &Sentence{Text: t.String(), Parts: b.parts}
}
