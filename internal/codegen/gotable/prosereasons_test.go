package gotable

import (
	"regexp"
	"strings"
	"testing"
)

// TestVariableLoadNamesTheRefusedForm pins docs/SPEC-TABLES.md §3 (THE FIRST
// BYTE) and §6.5's one-enum table: the variable <T>Load and <T>LoadBuilder
// name a refused form byte by its TableRefuseReason member, message_form_as_file
// for 2 and newer_form for a byte above the reader's form 1, and report.Err()
// answers that same member rather than prose or unknown_form. No emitted Reason
// literal is a prose sentence: a literal with a space is a member name and its
// ": " detail, never a phrase.
func TestVariableLoadNamesTheRefusedForm(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Root {
	n int32 = 1
}
`
	runGenerated(t, schema, variableLoadNamesProbe)
	for name, data := range generate(t, schema) {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		for _, literal := range variableReasonLiterals(string(data)) {
			if literal == "" || !strings.Contains(literal, " ") {
				continue
			}
			if !variableReasonDetail.MatchString(literal) {
				t.Errorf("%s: prose Reason literal %q: name the TableRefuseReason member", name, literal)
			}
		}
	}
}

var (
	variableReasonLiteral = regexp.MustCompile(`Reason\s*=\s*"([^"]*)"`)
	variableReasonDetail  = regexp.MustCompile(`^[a-z0-9_]+: `)
)

func variableReasonLiterals(source string) []string {
	matches := variableReasonLiteral.FindAllStringSubmatch(source, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}

const variableLoadNamesProbe = `package probe

import (
	"errors"
	"testing"
)

func refuseForm(t *testing.T, name string, wire []byte, wantReason string, want TableRefuseReason) {
	t.Helper()
	size := RootLoadMeasure(wire)
	if size < 0 {
		size = 0
	}
	var report TableReport
	if RootLoad(make([]byte, size), wire, &report) != nil {
		t.Fatalf("%s: Load accepted %v", name, wire)
	}
	if report.Reason != wantReason {
		t.Fatalf("%s: Load Reason = %q, want %q", name, report.Reason, wantReason)
	}
	if !errors.Is(report.Err(), want) {
		t.Fatalf("%s: Load Err() = %v, want errors.Is(err, %v)", name, report.Err(), want)
	}
	var builder RootBuilder
	if !builder.Init() {
		t.Fatal("builder init")
	}
	defer builder.Shutdown()
	var buildReport TableReport
	if RootLoadBuilder(&builder, wire, &buildReport) {
		t.Fatalf("%s: LoadBuilder accepted %v", name, wire)
	}
	if buildReport.Reason != wantReason {
		t.Fatalf("%s: LoadBuilder Reason = %q, want %q", name, buildReport.Reason, wantReason)
	}
	if !errors.Is(buildReport.Err(), want) {
		t.Fatalf("%s: LoadBuilder Err() = %v, want errors.Is(err, %v)", name, buildReport.Err(), want)
	}
}

func TestVariableLoadNamesTheRefusedForm(t *testing.T) {
	refuseForm(t, "form 2", []byte{2}, "message_form_as_file", TableRefuseMessageFormAsFile)
	refuseForm(t, "form 3", []byte{3}, "newer_form", TableRefuseNewerForm)
	refuseForm(t, "form 255", []byte{255}, "newer_form", TableRefuseNewerForm)
}
`
