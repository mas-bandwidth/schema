// Command hostilecorpus is the generator, drift check and worked-example Go
// port runner for the shared hostile-input corpus.
//
//	go run ./tools/hostilecorpus verify    # drift gate: committed manifest == engine
//	go run ./tools/hostilecorpus run       # Go port runner: report every verdict
//	go run ./tools/hostilecorpus generate  # rebuild the manifest from the engine
//
// Every per-port card (schema-<port>-runs-hostile-corpus) reads the same
// internal/ci/hostilecorpus/corpus directory and follows run's shape: decode
// each raw input with the carrier the manifest names, compare the verdict, the
// named reason and (for an accept) the decoded-value digest.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

func main() {
	root := flag.String("root", "", "repository root (default: current directory)")
	corpus := flag.String("corpus", "", "corpus directory (default: <root>/internal/ci/hostilecorpus/corpus)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: hostilecorpus [-root DIR] [-corpus DIR] verify|run|generate")
		os.Exit(2)
	}
	r := *root
	if r == "" {
		var err error
		r, err = os.Getwd()
		if err != nil {
			fatal(err)
		}
	}
	dir := *corpus
	if dir == "" {
		dir = filepath.Join(r, "internal", "ci", "hostilecorpus", "corpus")
	}
	if err := run(flag.Arg(0), r, dir); err != nil {
		fatal(err)
	}
}

func run(command, root, dir string) error {
	manifest, cases, err := hostilecorpus.LoadManifest(dir)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "hostilecorpus-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	switch command {
	case "verify":
		return hostilecorpus.Verify(root, tmp, dir, manifest)
	case "generate":
		got, err := hostilecorpus.Generate(root, tmp, dir, cases)
		if err != nil {
			return err
		}
		return hostilecorpus.WriteManifest(dir, hostilecorpus.Format(got))
	case "run":
		got, err := hostilecorpus.Generate(root, tmp, dir, cases)
		if err != nil {
			return err
		}
		want := hostilecorpus.Format(got)
		if want != manifest {
			for _, d := range hostilecorpus.Diff(manifest, want) {
				fmt.Fprintln(os.Stderr, d)
			}
			return fmt.Errorf("the Go port disagrees with the committed manifest")
		}
		for _, c := range cases {
			fmt.Printf("PASS %s\t%s\t%s\t%s\n", c.Name, c.Verdict, c.Reason, c.Digest)
		}
		fmt.Printf("%d cases: the Go port agrees with the committed manifest\n", len(cases))
		return nil
	default:
		return fmt.Errorf("unknown command %q (want verify, run or generate)", command)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
