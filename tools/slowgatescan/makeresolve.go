package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// makeFunctionCall is a make function call in a variable's value. A variable
// that holds one (`GO_ROWS := $(sort $(shell sed …))`) is a list make computes,
// not text this scan can read: expanded as written it tokenised into
// `$(shell`, `sed` and `expr`, each demanded as a test name (PR 4912). Its
// value is make's to say, so make is asked for it.
var makeFunctionCall = regexp.MustCompile(`\$[({](?:shell|sort|wordlist|words|word|firstword|lastword|filter|filter-out|subst|patsubst|strip|wildcard|foreach|if|addprefix|addsuffix|dir|notdir|basename|suffix|join|abspath|realpath|call|eval|value)[ \t,]`)

// resolveMakeFunctionVariables replaces, in vars, every variable whose value
// holds a make function call by the value make computes for it from the
// repository's own Makefile. Nothing is guessed: if make cannot answer, the
// scan fails by name. Every token the value then holds is still demanded to
// be a declared test by the proof-name audit, so this widens nothing.
func resolveMakeFunctionVariables(root string, vars map[string]string) error {
	var names []string
	for name, value := range vars {
		if makeFunctionCall.MatchString(value) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "slowgatescan-make-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }() // scratch only
	// A wrapper makefile (not --eval, which GNU make 3.81 lacks) that includes
	// the real one and adds a target printing each named variable on one line.
	wrapper := fmt.Sprintf("include %s\n\nslowgate-print-vars: ; @:$(foreach v,%s,$(info SLOWGATEVAR $(v)=$($(v))))\n",
		filepath.Join(absRoot, "Makefile"), strings.Join(names, " "))
	wf := filepath.Join(dir, "wrapper.mk")
	if err := os.WriteFile(wf, []byte(wrapper), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("make", "-s", "-C", absRoot, "-f", wf, "slowgate-print-vars")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("asking make for the value of %s: %w: %s", strings.Join(names, ", "), err, strings.TrimSpace(stderr.String()))
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "SLOWGATEVAR ")
		if !ok {
			continue
		}
		if k, v, ok := strings.Cut(rest, "="); ok {
			got[k] = strings.TrimSpace(v)
		}
	}
	for _, name := range names {
		v, ok := got[name]
		if !ok {
			return fmt.Errorf("make did not print a value for %s", name)
		}
		vars[name] = v
	}
	return nil
}
