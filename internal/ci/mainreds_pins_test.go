package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mainRedsSiblingPins is one CI sibling tag and the make variable that owns
// it. The make files are the authority: a pin that only a workflow names is a
// pin nothing stages against.
var mainRedsSiblingPins = []struct {
	ciEnv   string
	makeVar string
}{
	{"SERIALIZE_TAG", "SERIALIZE_PIN"},
	{"SERIALIZE_C_TAG", "SERIALIZE_C_PIN"},
	{"SERIALIZE_CS_TAG", "SERIALIZE_CS_PIN"},
	{"SERIALIZE_GO_TAG", "SERIALIZE_GO_PIN"},
	{"SERIALIZE_RS_TAG", "SERIALIZE_RS_PIN"},
	{"SERIALIZE_JS_TAG", "SERIALIZE_JS_PIN"},
}

var mainRedsWorkflowPinRE = regexp.MustCompile(`(?m)^  (SERIALIZE[A-Z0-9_]*_TAG|SERIALIZE_TAG):[ \t]*['"]?([^'"#\s]+)['"]?[ \t]*$`)

// TestMainRedsPinsMatchCIFull holds every sibling-runtime tag ci-full.yml
// clones to the pin make itself resolves, and refuses a pin that has no make
// consumer. The value is read by RUNNING make, not by matching the first
// literal assignment: an appended `SERIALIZE_PIN := v0.0.0` or an included
// file that redefines it changes what make stages, and has to turn this test
// red. The two subtests are that negative control.
func TestMainRedsPinsMatchCIFull(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	workflow, ok := workflows(t, root)["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is absent")
	}
	ciPins := mainRedsWorkflowPins(t, workflow)
	makePins := mainRedsEffectivePins(t, root, nil)

	known := map[string]bool{}
	for _, pin := range mainRedsSiblingPins {
		known[pin.ciEnv] = true
		ci, ok := ciPins[pin.ciEnv]
		if !ok {
			t.Errorf("ci-full.yml does not pin %s", pin.ciEnv)
			continue
		}
		mk, ok := makePins[pin.makeVar]
		if !ok || mk == "" {
			t.Errorf("the make files resolve no %s, so ci-full.yml's %s=%s has no owner",
				pin.makeVar, pin.ciEnv, ci)
			continue
		}
		if ci != mk {
			t.Errorf("ci-full.yml %s=%s != the make files' effective %s=%s",
				pin.ciEnv, ci, pin.makeVar, mk)
		}
	}
	for name := range ciPins {
		if !known[name] {
			t.Errorf("ci-full.yml pins %s, which no make variable owns; add it here and give it a consumer", name)
		}
	}

	makeText := mainRedsMakeText(t, root)
	for _, pin := range mainRedsSiblingPins {
		if !strings.Contains(makeText, "$("+pin.makeVar+")") {
			t.Errorf("%s is declared but never consumed by a make rule; a pin that only this test reads cannot bind a sibling checkout", pin.makeVar)
		}
	}

	t.Run("a later assignment overrides the pin", func(t *testing.T) {
		t.Parallel()
		extra := filepath.Join(t.TempDir(), "override.mk")
		if err := os.WriteFile(extra, []byte("SERIALIZE_PIN := v0.0.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pins := mainRedsEffectivePins(t, root, []string{extra})
		if got := pins["SERIALIZE_PIN"]; got != "v0.0.0" {
			t.Fatalf("a later := assignment did not win: effective SERIALIZE_PIN=%q, want v0.0.0", got)
		}
		if pins["SERIALIZE_PIN"] == ciPins["SERIALIZE_TAG"] {
			t.Fatalf("the override resolved to ci-full.yml's pin %q, so the guard cannot see it", pins["SERIALIZE_PIN"])
		}
	})

	t.Run("an included file overrides the pin", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		child := filepath.Join(dir, "child.mk")
		if err := os.WriteFile(child, []byte("SERIALIZE_GO_PIN := v9.9.9\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(dir, "parent.mk")
		if err := os.WriteFile(parent, []byte("include "+child+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pins := mainRedsEffectivePins(t, root, []string{parent})
		if got := pins["SERIALIZE_GO_PIN"]; got != "v9.9.9" {
			t.Fatalf("an included assignment did not win: effective SERIALIZE_GO_PIN=%q, want v9.9.9", got)
		}
		if pins["SERIALIZE_GO_PIN"] == ciPins["SERIALIZE_GO_TAG"] {
			t.Fatalf("the included override resolved to ci-full.yml's pin %q, so the guard cannot see it", pins["SERIALIZE_GO_PIN"])
		}
	})
}

// mainRedsWorkflowPins reads the top-level env pins ci-full.yml clones from.
func mainRedsWorkflowPins(t *testing.T, workflow string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range mainRedsWorkflowPinRE.FindAllStringSubmatch(workflow, -1) {
		name, value := m[1], m[2]
		if previous, ok := out[name]; ok && previous != value {
			t.Fatalf("ci-full.yml pins %s twice: %q and %q", name, previous, value)
		}
		out[name] = value
	}
	return out
}

// mainRedsEffectivePins asks make for the value each pin has after the
// Makefile and every extra file are read, in order. The value is whatever make
// would use, so a later :=, an override and an include all count.
func mainRedsEffectivePins(t *testing.T, root string, extra []string) map[string]string {
	t.Helper()
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("this gate resolves the pins by running make: %v", err)
	}
	probe := filepath.Join(t.TempDir(), "mainreds-pins.mk")
	var text strings.Builder
	text.WriteString(".PHONY: __mainreds_print_pins\n__mainreds_print_pins:\n\t@printf '%s\\n'")
	for _, pin := range mainRedsSiblingPins {
		fmt.Fprintf(&text, " '%s=$(%s)'", pin.makeVar, pin.makeVar)
	}
	text.WriteString("\n")
	if err := os.WriteFile(probe, []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-s", "--no-print-directory", "-f", "Makefile"}
	for _, path := range extra {
		args = append(args, "-f", path)
	}
	args = append(args, "-f", probe, "__mainreds_print_pins")
	cmd := exec.Command(makeBin, args...)
	cmd.Dir = root
	cmd.Env = mainRedsCleanEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make %v: %v\n%s", args, err, output)
	}

	pins := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("make printed %q, want NAME=VALUE", line)
		}
		pins[name] = value
	}
	return pins
}

// mainRedsMakeText is every make input, so a consumer in an included .mk
// counts the same as one in the root Makefile.
func mainRedsMakeText(t *testing.T, root string) string {
	t.Helper()
	var text strings.Builder
	paths := []string{filepath.Join(root, "Makefile")}
	for _, pattern := range []string{"make/*.mk", "make/checks/*.mk"} {
		files, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, files...)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text.Write(data)
		text.WriteString("\n")
	}
	return text.String()
}

// mainRedsCleanEnv drops the pin variables so a developer's shell cannot
// change what the probe resolves. make reads the environment below a file
// assignment, but an explicitly exported pin would still mask a missing one.
func mainRedsCleanEnv() []string {
	drop := map[string]bool{"MAKEFLAGS": true, "MFLAGS": true}
	for _, pin := range mainRedsSiblingPins {
		drop[pin.makeVar] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			out = append(out, kv)
		}
	}
	return out
}
