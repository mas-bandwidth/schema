// Command errdiscard lists every error the Go programs under cmd/ and tools/
// drop (internal/errdiscard). It exits 1 when one names no reason, or when it
// read nothing; -all also lists the kept discards with their reasons.
//
//	go run ./tools/errdiscard [-all] [patterns...]   (default ./cmd/... ./tools/...)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/errdiscard"
)

func main() {
	all := flag.Bool("all", false, "also list the discards that name a reason")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./cmd/...", "./tools/..."}
	}
	findings, c, err := errdiscard.Check(".", patterns...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errdiscard: %v\n", err)
		os.Exit(2)
	}
	refused := 0
	for _, f := range findings {
		switch {
		case f.Reason == "":
			refused++
			fmt.Println(f)
		case *all:
			fmt.Printf("%s (kept: %s)\n", f, f.Reason)
		}
	}
	fmt.Fprintf(os.Stderr, "errdiscard: %d packages, %d files, %d calls that return an error; %d discards, %d with no reason\n",
		c.Packages, c.Files, c.Calls, len(findings), refused)
	if c.Packages == 0 || c.Calls == 0 {
		fmt.Fprintln(os.Stderr, "errdiscard: the run looked at nothing: a gate over nothing is not a pass")
		os.Exit(1)
	}
	if refused > 0 {
		fmt.Fprintln(os.Stderr, "errdiscard: handle each error above, or name why it is safe to drop in an `// errdiscard: <reason>` comment on its line")
		os.Exit(1)
	}
}
