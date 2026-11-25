// Command gotestselect prints the `go test -run` pattern naming every
// top-level test of the given packages that a row's other rows do not name
// (internal/testselect): the complement, listed by name, so no row needs a
// -skip. It leaves out tests whose tier is nightly unless -tier all.
//
//	go run ./tools/gotestselect [-run <regexp>] [-not <regexp>] [-tier pr|all] [-tags <tags>] <package>...
//
// The packages are anything `go list` takes. An empty selection fails.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testselect"
)

func main() {
	run := flag.String("run", "", "keep only the tests this matches (go test's -run matching, on the top-level name)")
	not := flag.String("not", "", "leave out the tests this matches (go test's -run matching, on the top-level name)")
	tier := flag.String("tier", "pr", "pr leaves out nightly-tier tests; all keeps them")
	tags := flag.String("tags", "", "build tags, as go test -tags")
	flag.Parse()
	if flag.NArg() == 0 || (*tier != "pr" && *tier != "all") {
		fmt.Fprintln(os.Stderr, "usage: gotestselect [-run <regexp>] [-not <regexp>] [-tier pr|all] [-tags <tags>] <package>...")
		os.Exit(2)
	}
	runRe, notRe := compile(*run), compile(*not)
	root, err := goList("-m", "-f", "{{.Dir}}")
	if err != nil {
		fail(err)
	}
	args := []string{"-f", "{{.Dir}}"}
	if *tags != "" {
		args = append(args, "-tags", *tags)
	}
	dirs, err := goList(append(args, flag.Args()...)...)
	if err != nil {
		fail(err)
	}
	ctx := build.Default
	if *tags != "" {
		ctx.BuildTags = strings.Split(*tags, ",")
	}
	pattern, err := testselect.Select(ctx, root[0], dirs, runRe, notRe, *tier == "all")
	if err != nil {
		fail(err)
	}
	fmt.Println(pattern)
}

func goList(args ...string) ([]string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.Fields(out.String()), nil
}

// compile is nil for an empty pattern.
func compile(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		fail(err)
	}
	return re
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gotestselect:", err)
	os.Exit(1)
}
