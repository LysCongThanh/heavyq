package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// storyResult is the story computed beside the text status.
type storyResult struct {
	sentences []snapshot.Sentence
	err       error
}

// startStory builds a snapshot in the background and returns its story. The
// channel is buffered, so an abandoned result never blocks the goroutine.
func startStory(cfg *config.Config, probes *probeCache) <-chan storyResult {
	ch := make(chan storyResult, 1)
	go func() {
		s, err := buildSnapshot(cfg, probes, cpuSampleInterval)
		ch <- storyResult{sentences: s.Story, err: err}
	}()
	return ch
}

// printStory writes the STORY section. When the story cannot be built the
// rest of `status` still stands; the failure goes to stderr.
func printStory(w, stderr io.Writer, r storyResult) {
	if r.err != nil {
		fmt.Fprintln(stderr, "heavyq: story unavailable:", r.err)
		return
	}
	fmt.Fprintln(w, "STORY")
	for _, s := range r.sentences {
		fmt.Fprintf(w, "  %s\n", s.Text)
	}
}
