// Command nightlyred announces a red ci-full nightly on the one issue
// labelled nightly-red, and closes that issue when the nightly is green again.
//
//	go run ./tools/nightlyred -repo mas-bandwidth/schema -run <run id>
//
// .github/workflows/nightly-red.yml runs it when a ci-full run completes. The
// decision is internal/ci/nightlyred.Announce, tested from recorded runs in
// internal/ci/nightly_red_test.go; this file only hands it the gh CLI, which
// reads GH_TOKEN from the environment.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ci/nightlyred"
)

type ghCLI struct{}

func (ghCLI) API(path string, paginate bool) ([]byte, error) {
	// job logs carry the runners' colour codes; gh refuses to print those
	// unless asked, and FirstError takes them off
	args := []string{"api", "--allow-escape-sequences"}
	if paginate {
		args = append(args, "--paginate")
	}
	return run(append(args, path), nil)
}

func (ghCLI) Run(args []string, stdin []byte) ([]byte, error) {
	return run(args, stdin)
}

func run(args []string, stdin []byte) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func main() {
	repo := flag.String("repo", "", "owner/name of the repository")
	runID := flag.Int64("run", 0, "id of the completed ci-full run")
	flag.Parse()
	if *repo == "" || *runID <= 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: nightlyred -repo owner/name -run <run id>")
		os.Exit(2)
	}
	if err := nightlyred.Announce(ghCLI{}, *repo, *runID, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "nightlyred:", err)
		os.Exit(1)
	}
}
