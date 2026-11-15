package hostilecorpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type unsafeInput struct {
	name  string
	input string
}

// unsafeInputs are manifest inputs a conforming corpus cannot hold: the part
// after "inputs/" is not a local slash-separated path.
var unsafeInputs = []unsafeInput{
	{"climbs-out", "inputs/../outside.bin"},
	{"absolute", "inputs//outside.bin"},
	{"backslash", "inputs/dir\\outside.bin"},
	{"empty", "inputs/"},
}

// reporter is the part of testing.TB the input assertion needs.
type reporter interface {
	Errorf(format string, args ...any)
}

// assertRefused is the one escape assertion both the test and its negative
// control run: it fails by case name for every input the check accepts.
func assertRefused(rep reporter, check func(unsafeInput) error) {
	for _, row := range unsafeInputs {
		if err := check(row); err == nil {
			rep.Errorf("%s: input %q was accepted", row.name, row.input)
		}
	}
}

// oldPrefixOnly is a copy of the prefix-only check the package used to make:
// any text beginning with "inputs/" was taken as an input name.
func oldPrefixOnly(input string) error {
	if strings.TrimPrefix(input, "inputs/") == input {
		return fmt.Errorf("input %q is not under inputs/", input)
	}
	return nil
}

func manifestWithInput(name, input string) string {
	return fmt.Sprintf("%s\twire\ttables/examples\tRoot\t%s\thand\trefuse\tdamaged\t-\n", name, input)
}

// TestManifestInputStaysInsideCorpus requires every manifest input to name a
// file inside the corpus's inputs/ directory, in Parse and in Payload.
func TestManifestInputStaysInsideCorpus(t *testing.T) {
	t.Parallel()

	assertRefused(t, func(row unsafeInput) error {
		_, err := Parse(manifestWithInput(row.name, row.input))
		return err
	})

	for _, row := range unsafeInputs {
		_, err := Parse(manifestWithInput(row.name, row.input))
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), row.name) {
			t.Errorf("%s: Parse error %q does not name the case", row.name, err)
		}
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	const outside = "outside payload that must not be read"
	if err := os.WriteFile(filepath.Join(dir, "outside.bin"), []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, row := range unsafeInputs {
		payload, err := Payload(dir, row.input)
		if err == nil {
			t.Errorf("%s: Payload accepted input %q and returned %q", row.name, row.input, payload)
			continue
		}
		if payload != nil {
			t.Errorf("%s: Payload returned a payload for input %q", row.name, row.input)
		}
		if !strings.Contains(err.Error(), strconv.Quote(row.input)) {
			t.Errorf("%s: Payload error %q does not name input %q", row.name, err, row.input)
		}
	}

	const clean = "clean payload"
	if err := os.WriteFile(filepath.Join(dir, "inputs", "clean.bin"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	cases, err := Parse(manifestWithInput("clean", "inputs/clean.bin"))
	if err != nil {
		t.Fatalf("clean input: Parse: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("clean input: parsed %d cases, want 1", len(cases))
	}
	payload, err := Payload(dir, cases[0].Input)
	if err != nil {
		t.Fatalf("clean input: Payload: %v", err)
	}
	if string(payload) != clean {
		t.Errorf("clean input: payload %q, want %q", payload, clean)
	}
}

// TestManifestInputStaysInsideCorpusNegativeControl runs the same rows through
// a copy of the old prefix-only check and requires the escape assertion to go
// red, by name, for every one.
func TestManifestInputStaysInsideCorpusNegativeControl(t *testing.T) {
	t.Parallel()

	rec := &recordingReporter{}
	assertRefused(rec, func(row unsafeInput) error {
		return oldPrefixOnly(row.input)
	})
	if len(rec.lines) != len(unsafeInputs) {
		t.Errorf("prefix-only check went red %d times, want %d: %v", len(rec.lines), len(unsafeInputs), rec.lines)
	}
	for _, row := range unsafeInputs {
		found := false
		for _, line := range rec.lines {
			if strings.Contains(line, row.name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: prefix-only check did not turn the escape assertion red by name", row.name)
		}
	}
}

type recordingReporter struct {
	lines []string
}

func (r *recordingReporter) Errorf(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}
