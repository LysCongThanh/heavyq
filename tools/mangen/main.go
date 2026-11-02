// Command mangen writes the heavyq(1) manual page, generated from the command
// table in internal/cli. The release build (goreleaser "before" hook) runs it
// and ships the result in every archive:
//
//	go run ./tools/mangen -version 1.2.3 -date 2026-11-02 -o man/heavyq.1
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cli"
)

func main() {
	out := flag.String("o", "", "write here instead of stdout")
	version := flag.String("version", "dev", "version shown in the footer")
	dateFlag := flag.String("date", "", "date shown in the footer (default: none)")
	flag.Parse()

	// -date is the commit date goreleaser passes ("2006-01-02" or RFC 3339); only the day is kept, so
	// the page is reproducible.
	date := ""
	if *dateFlag != "" {
		if len(*dateFlag) < 10 {
			fmt.Fprintln(os.Stderr, "mangen: bad -date", *dateFlag)
			os.Exit(2)
		}
		day, err := time.Parse("2006-01-02", (*dateFlag)[:10])
		if err != nil {
			fmt.Fprintln(os.Stderr, "mangen: bad -date:", err)
			os.Exit(2)
		}
		date = day.Format("2006-01-02")
	}

	var page bytes.Buffer
	cli.WriteManPage(&page, *version, date)
	if *out == "" {
		_, _ = os.Stdout.Write(page.Bytes())
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, page.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
}
