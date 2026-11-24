// THE RETAIN-REFUSAL SURFACE (docs/SPEC-TABLES.md §6.6): "a retention buffer
// belongs to one loaded region, and the next LoadRetain into it resets both
// stores." A refused retaining read is such a next load. It hands the
// caller's store back holding nothing, so a later save through that store
// writes none of an earlier read's retained records and none of the refused
// read's own partial captures.
//
// One row is a sequence through ONE store:
//
//	retain-refusal <case> <unit> <root> <first wire> <refused wire>
//
// LoadRetain of the first wire (it reads and retains), SaveRetain of that
// value (the bytes carry its retained records), LoadRetain of the refused wire
// into the same store (it refuses), and SaveRetain of the first value again.
// The second save owes the first value's known fields alone, or a refusal
// where the leg binds a store to the root it loaded and the refused load
// unbound it: either way nothing the store held before the refusal is written.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// RetainRefusalDir holds the two saves each `retain-refusal` row owes,
// generated from the engine by `make conformance-generate`.
const RetainRefusalDir = "testdata/conformance/tables/retain-refusal"

// RetainRefusal is one `retain-refusal` row.
type RetainRefusal struct {
	Name    string
	Unit    string
	Root    string
	First   string
	Refused string
}

// Before is the save through the store after the first read: the first value
// with its retained records.
func (r RetainRefusal) Before() string { return RetainRefusalDir + "/" + r.Name + ".before.bin" }

// After is the save through the store after the refused read: the first
// value's known fields alone.
func (r RetainRefusal) After() string { return RetainRefusalDir + "/" + r.Name + ".after.bin" }

func parseRetainRefusal(f []string, where string) (RetainRefusal, error) {
	if len(f) != 6 {
		return RetainRefusal{}, fmt.Errorf("%s: retain-refusal takes case, unit, root, first wire, refused wire", where)
	}
	return RetainRefusal{Name: f[1], Unit: f[2], Root: f[3], First: f[4], Refused: f[5]}, nil
}

func (r RetainRefusal) derivedLine() string {
	return fmt.Sprintf("retain-refusal %s %s %s %s %s\n", r.Name, r.Unit, r.Root, r.First, r.Refused)
}

// The answer a leg writes in `<case>`: the first read's verdict, the refused
// read's verdict, and what the second save did. The first save's bytes ride
// in `<case>.before` and the second's in `<case>.wire`.
const (
	retainRefusalSaved   = "read\nrefused\nsaved\n"
	retainRefusalUnbound = "read\nrefused\nsave-refused\n"
)

// generateRetainRefusals writes both saves of every row from the engine, and
// holds the row to being a test: the first read retains (the first save is
// not the plain save), the refused read is refused by the engine's own
// retaining read (which resets the store, retain.go), and the save after it is
// exactly the plain save of the first value.
func generateRetainRefusals(u *units, m *Manifest) error {
	if err := os.MkdirAll(RetainRefusalDir, 0o755); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, r := range m.RetainRefusals {
		unit, err := u.get(r.Unit)
		if err != nil {
			return err
		}
		root := unit.Tables[r.Root]
		if root == nil {
			return fmt.Errorf("retain-refusal %s: root %q names no table", r.Name, r.Root)
		}
		first, err := os.ReadFile(r.First)
		if err != nil {
			return fmt.Errorf("retain-refusal %s: %w", r.Name, err)
		}
		refused, err := os.ReadFile(r.Refused)
		if err != nil {
			return fmt.Errorf("retain-refusal %s: %w", r.Name, err)
		}
		model := tabletext.NewModel(unit)
		const roomy = 1 << 20
		store := &tablewire.Retain{Capacity: roomy, IdCapacity: roomy / 8}
		value := model.New(root)
		var report tabletext.Report
		if ok, err := strictDecodeRetain(model, value, first, store, &report); !ok {
			return fmt.Errorf("retain-refusal %s: the engine refuses the first wire: %v", r.Name, err)
		}
		var saveReport tabletext.Report
		before, err := tablewire.EncodeRetain(model, value, store, &saveReport)
		if err != nil {
			return fmt.Errorf("retain-refusal %s: first save: %w", r.Name, err)
		}
		plain, err := tablewire.EncodeVariable(model, value)
		if err != nil {
			return fmt.Errorf("retain-refusal %s: plain save: %w", r.Name, err)
		}
		if bytes.Equal(before, plain) {
			return fmt.Errorf("retain-refusal %s: the first wire retains nothing, so the row tests nothing", r.Name)
		}
		var refusedReport tabletext.Report
		if ok, _ := tablewire.DecodeRetain(model, model.New(root), refused, store, &refusedReport); ok {
			return fmt.Errorf("retain-refusal %s: the engine's retaining read accepts the refused wire", r.Name)
		}
		var afterReport tabletext.Report
		after, err := tablewire.EncodeRetain(model, value, store, &afterReport)
		if err != nil {
			return fmt.Errorf("retain-refusal %s: second save: %w", r.Name, err)
		}
		if !bytes.Equal(after, plain) {
			return fmt.Errorf("retain-refusal %s: the engine's save after the refusal is not the plain save (%d bytes, plain %d)", r.Name, len(after), len(plain))
		}
		if err := writeFileAtomic(r.Before(), before); err != nil {
			return err
		}
		if err := writeFileAtomic(r.After(), after); err != nil {
			return err
		}
		keep[filepath.Base(r.Before())] = true
		keep[filepath.Base(r.After())] = true
		fmt.Printf("conformance: %-22s retain-refusal\n", r.Name)
	}
	return pruneValues(RetainRefusalDir, keep)
}

// scoreRetainRefusal scores one row: the verdict lines, the first save
// (`<case>.before`) and the second (`<case>.wire`, or none when the leg
// refused the save through the unbound store).
func scoreRetainRefusal(answer string, e expectation, r *result) {
	fail := func(format string, args ...any) {
		r.failures = append(r.failures, fmt.Sprintf("%s: ", e.name)+fmt.Sprintf(format, args...))
		r.mark(e.name, outcomeFail)
	}
	got, err := os.ReadFile(answer)
	if err != nil {
		if reason, aerr := os.ReadFile(answer + ".absent"); aerr == nil {
			if len(bytes.TrimSpace(reason)) == 0 {
				// an EMPTY `.absent` is a missing feature for this leg
				r.missing++
				r.mark(e.name, outcomeAbsent)
				return
			}
			fail("the leg refused (%s) where the row owes an answer; a missing codec writes an EMPTY .absent", firstLine(reason))
			return
		}
		fail("the driver wrote nothing")
		return
	}
	if string(got) != retainRefusalSaved && string(got) != retainRefusalUnbound {
		fail("the sequence answered %q, want %q or %q", got, retainRefusalSaved, retainRefusalUnbound)
		return
	}
	before, err := os.ReadFile(answer + ".before")
	if err != nil {
		fail("the driver wrote no first save (%s.before)", e.name)
		return
	}
	if !bytes.Equal(before, e.want) {
		fail("the first save is not the first value with its retained records: %s", describeDiff(e.name+".before", e.want, before))
		return
	}
	wire, werr := os.ReadFile(answer + ".wire")
	if string(got) == retainRefusalUnbound {
		if werr == nil {
			fail("the leg answered save-refused and wrote %d bytes of a second save", len(wire))
			return
		}
		r.pass++
		r.mark(e.name, outcomePass)
		return
	}
	if werr != nil {
		fail("the leg answered saved and wrote no second save (%s.wire)", e.name)
		return
	}
	if bytes.Equal(wire, before) {
		fail("the save after the refused read wrote the earlier read's retained records (%d bytes, the first save's)", len(wire))
		return
	}
	if !bytes.Equal(wire, e.save) {
		fail("the save after the refused read is not the first value's known fields alone: %s", describeDiff(e.name+".wire", e.save, wire))
		return
	}
	r.pass++
	r.mark(e.name, outcomePass)
}

// RetainRefusalOwed is the exact-set ledger of the retain-refusal rows a leg
// does not answer yet, beside the manifest. One line per leg and case:
//
//	<leg> <case> owed: <cause>
//
// A ledgered case is scored as usual and then held to its line: an answer
// that fails is owed (uncredited, named in the run's output, never red), and
// an answer that PASSES is red until its line is removed. A case with no line
// is scored as every other row: a wrong or missing answer is red.
const RetainRefusalOwed = "retain-refusal-owed.txt"

// retainRefusalLedger maps "<leg> <case>" to the cause the ledger names.
type retainRefusalLedger map[string]string

// loadRetainRefusalLedger reads the ledger beside manifestPath. A manifest
// with no ledger beside it owes nothing, so every row is held to its answer.
// A line that is not `<leg> <case> owed: <cause>`, names a case the manifest
// has no retain-refusal row for, or repeats a leg and case is an error.
func loadRetainRefusalLedger(manifestPath string, m *Manifest) (retainRefusalLedger, error) {
	path := filepath.Join(filepath.Dir(manifestPath), RetainRefusalOwed)
	text, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return retainRefusalLedger{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := map[string]bool{}
	for _, rr := range m.RetainRefusals {
		rows[rr.Name] = true
	}
	ledger := retainRefusalLedger{}
	for i, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		where := fmt.Sprintf("%s:%d", path, i+1)
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "owed:" {
			return nil, fmt.Errorf("%s: want `<leg> <case> owed: <cause>`, got %q", where, line)
		}
		if !rows[f[1]] {
			return nil, fmt.Errorf("%s: %s names no retain-refusal row in %s", where, f[1], manifestPath)
		}
		key := f[0] + " " + f[1]
		if _, dup := ledger[key]; dup {
			return nil, fmt.Errorf("%s: %s is ledgered twice", where, key)
		}
		ledger[key] = strings.Join(f[3:], " ")
	}
	return ledger, nil
}

// owes says whether lang's answer to the case is ledgered, and why.
func (l retainRefusalLedger) owes(lang, surface, name string) (string, bool) {
	if surface != "retain-refusal" {
		return "", false
	}
	why, ok := l[lang+" "+name]
	return why, ok
}

// scoreOwedRetainRefusal scores a ledgered case: a failing answer is owed and
// said, a passing one is red until its ledger line goes.
func scoreOwedRetainRefusal(w io.Writer, lang string, answer string, e expectation, why string, r *result) {
	held := &result{}
	scoreRetainRefusal(answer, e, held)
	if held.pass > 0 {
		r.failures = append(r.failures, fmt.Sprintf(
			"%s: ledgered `%s %s owed: %s` in %s, and the leg answers it correctly: remove the line",
			e.name, lang, e.name, why, RetainRefusalOwed))
		r.mark(e.name, outcomeFail)
		return
	}
	r.missing++
	r.mark(e.name, outcomeOwed)
	got := "absent"
	if len(held.failures) > 0 {
		got = held.failures[0]
	}
	_, _ = fmt.Fprintf(w, "conformance OWED %s / retain-refusal: %s owed: %s (it answered: %s)\n", lang, e.name, why, got)
}
