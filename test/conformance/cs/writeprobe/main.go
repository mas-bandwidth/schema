// Command writeprobe writes the shared corpus's probe schema, which is not a
// file in the checkout: internal/ci/hostilecorpus holds it as the one Go string
// the engine's generator writes to a temporary directory. A port that needs a
// generated unit for the count_refusal case gets the same text from the same
// constant here, so the corpus keeps one home for it.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

func main() {
	out := flag.String("out", "", "path to write the probe schema to")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: writeprobe -out PATH")
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(hostilecorpus.ProbeSchema), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
