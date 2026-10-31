package compiler

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM IS FIXED TABLES ONLY, IN EVERY BACKEND (docs/SPEC-TABLES.md
// §3.3). Glenn, 2026-09-09: "and enforce only fixed tables can use message
// form. this saves more work." and 2026-10-31: "only fixed tables support
// message form (bitpacked)". This is the lock: every registered target
// generates one unit that holds a fixed table beside three variable tables
// (one pointered, one it points at, one with no pointer), and no line of any
// target's output names a variable table beside the message form. A backend
// that grows a message verb, a body codec, a slot list, a region runtime or a
// refusal stub for a variable table turns this red by name.

const messageFormLockSource = `package probe

fixed table Fixedroot
{
    n int32
    m uint8
}

table Varleaf
{
    v int32
}

table Varroot
{
    leaf *Varleaf
    k int32
}

table Varplain
{
    s int32
}
`

// messageFormLockVariable is the variable tables' names, lowered: a line that
// holds one of them and the word "message" is a variable-table message path.
var messageFormLockVariable = []string{"varleaf", "varroot", "varplain"}

// messageFormLockNoForm are the targets that carry no message form at all
// (docs/SPEC-TABLES.md §3.3, "WHERE THE FORM IS CARRIED"), so their output is
// not required to name the fixed root's batch.
var messageFormLockNoForm = map[string]bool{"goose": true}

// variableMessageViolations names every line of one target's output that puts
// a variable table beside the message form.
func variableMessageViolations(target string, files map[string][]byte) []string {
	var out []string
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lowerName := strings.ToLower(name)
		fileIsVariable := false
		for _, v := range messageFormLockVariable {
			if strings.Contains(lowerName, v) {
				fileIsVariable = true
			}
		}
		for i, line := range strings.Split(string(files[name]), "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "message") {
				continue
			}
			variable := fileIsVariable
			for _, v := range messageFormLockVariable {
				if strings.Contains(lower, v) {
					variable = true
				}
			}
			if variable {
				out = append(out, fmt.Sprintf("%s: %s:%d puts a variable table beside the message form: %s", target, name, i+1, strings.TrimSpace(line)))
			}
		}
	}
	return out
}

// fixedMessagePresent reports whether one target's output names the fixed
// root beside the message form, which is what keeps the lock from passing on
// output that carries no message form at all.
func fixedMessagePresent(files map[string][]byte) bool {
	for _, data := range files {
		for _, line := range strings.Split(string(data), "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "fixedroot") && strings.Contains(lower, "message") {
				return true
			}
		}
	}
	return false
}

func generateMessageFormLock(t *testing.T) (map[string]map[string][]byte, []string) {
	t.Helper()
	c := New()
	u := unitFromSource(t, messageFormLockSource)
	out := map[string]map[string][]byte{}
	for _, target := range c.Targets() {
		files, err := c.Generate(u, target, Options{})
		if err != nil {
			t.Fatalf("%s refused the lock's unit: %v", target, err)
		}
		out[target] = files
	}
	// THE TWO TAGGED LEGS (compiler/target_moo.go, target_goose.go build only
	// under their schema_leg_* tags), through their table emitters directly
	for target, generate := range map[string]func(*ir.Unit) (map[string][]byte, error){"moo": mootable.Generate, "goose": goosetable.Generate} {
		if _, ok := out[target]; ok {
			continue
		}
		files, err := generate(u)
		if err != nil {
			t.Fatalf("%s refused the lock's unit: %v", target, err)
		}
		out[target] = files
	}
	targets := make([]string, 0, len(out))
	for target := range out {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	if len(targets) != 15 {
		t.Fatalf("%d backends generated (%v); the lock holds all fifteen", len(targets), targets)
	}
	return out, targets
}

// TestNoVariableTableMessagePath is the lock across every backend.
func TestNoVariableTableMessagePath(t *testing.T) {
	outputs, targets := generateMessageFormLock(t)
	for _, target := range targets {
		for _, v := range variableMessageViolations(target, outputs[target]) {
			t.Error(v)
		}
		if !messageFormLockNoForm[target] && !fixedMessagePresent(outputs[target]) {
			t.Errorf("%s: no line names the fixed root beside the message form, so the lock reads output that carries no message form (docs/SPEC-TABLES.md §3.3)", target)
		}
	}
}

// TestNoVariableTableMessagePathNegativeControl: the lock's check over every
// target's real output with one variable-table message line planted in it;
// the plant must be named for every target, and the clean output must not be.
func TestNoVariableTableMessagePathNegativeControl(t *testing.T) {
	outputs, targets := generateMessageFormLock(t)
	for _, target := range targets {
		files := outputs[target]
		if got := variableMessageViolations(target, files); len(got) != 0 {
			t.Fatalf("%s: the clean output is not clean: %v", target, got)
		}
		planted := map[string][]byte{}
		var first string
		for name, data := range files {
			planted[name] = data
			if first == "" || name < first {
				first = name
			}
		}
		planted[first] = append(append([]byte(nil), files[first]...), []byte("\nVarrootLoadMessages\n")...)
		got := strings.Join(variableMessageViolations(target, planted), "\n")
		if !strings.Contains(got, "VarrootLoadMessages") {
			t.Errorf("NEGATIVE CONTROL FAILED: %s: the planted variable-table message verb was not named", target)
		}
	}
}
