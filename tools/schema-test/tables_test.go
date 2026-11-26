package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

var updateTables = flag.Bool("update-tables", false, "rewrite the registry's tables rows from the test/tables corpus")

// tablesRowLine is a row as the registry file holds it.
func tablesRowLine(t testreg.Test) (string, error) {
	t.Line = 0
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return `{"rec":"test",` + string(b[1:]), nil
}

// TestTheRegistryHoldsEveryTablesCase holds the registry's tables rows
// to the corpus field for field: a case with no row, a row with no case
// and a row that differs are each red by key. -update-tables rewrites
// them (keeping each row's measured seconds).
func TestTheRegistryHoldsEveryTablesCase(t *testing.T) {
	path := "../../" + testreg.DefaultPath
	reg, err := testreg.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tablesRows("../..", reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the test/tables corpus gives no rows")
	}
	have := map[string]testreg.Test{}
	for _, row := range reg.Tests {
		if row.Family == tablesFamily {
			have[row.Key] = row
		}
	}
	if *updateTables {
		if err := rewriteTablesRows(path, want, have); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, w := range want {
		h, ok := have[w.Key]
		if !ok {
			t.Errorf("%s: the case %s has no registry row (go test ./tools/schema-test -run TestTheRegistryHoldsEveryTablesCase -args -update-tables)", w.Key, w.Source)
			continue
		}
		delete(have, w.Key)
		w.Seconds, w.Line = h.Seconds, h.Line
		wl, err := tablesRowLine(w)
		if err != nil {
			t.Fatal(err)
		}
		hl, err := tablesRowLine(h)
		if err != nil {
			t.Fatal(err)
		}
		if wl != hl {
			t.Errorf("%s:%d: the row is not the corpus's:\n have %s\n want %s", path, h.Line, hl, wl)
		}
	}
	for key, h := range have {
		t.Errorf("%s:%d: %s is a tables row the corpus has no case for", path, h.Line, key)
	}
}

// rewriteTablesRows drops every tables row from the registry file and
// appends want, each keeping the seconds its old row measured.
func rewriteTablesRows(path string, want []testreg.Test, have map[string]testreg.Test) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		var head struct{ Rec, Family string }
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			return err
		}
		if head.Rec == "test" && head.Family == tablesFamily {
			continue
		}
		out.WriteString(line + "\n")
	}
	for _, w := range want {
		w.Seconds = have[w.Key].Seconds
		l, err := tablesRowLine(w)
		if err != nil {
			return err
		}
		out.WriteString(l + "\n")
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// TestTablesVerdictsFollowTheCorpus: each place's cases take the schema
// and verdict the corpus holds, and a shape no verdict holds is refused.
func TestTablesVerdictsFollowTheCorpus(t *testing.T) {
	for _, c := range []struct {
		dir, name, wantCase, wantSchema, wantVerdict string
	}{
		{"test/tables", "A1", "a1", "test/tables/A1.schema", tablesAccept},
		{"test/tables", "view_go", "view-go", "", tablesPresent},
		{"test/tables/enumbound", "BodyMax", "enumbound-bodymax", "test/tables/enumbound/BodyMax.schema", tablesRefuse},
		{"test/tables/enumbound", "ControlPlain", "enumbound-controlplain", "test/tables/enumbound/ControlPlain.schema", tablesAccept},
		{"test/tables/fnz", "negative_zero", "fnz-negative-zero", "test/tables/FNZ.schema", tablesAccept},
		{"test/tables/r14/AtInt64", "Root", "r14-atint64-root", "test/tables/r14/AtInt64.schema", tablesAccept},
		{"test/tables/r17/FlagsPast", "EvoDoc", "r17-flagspast-evodoc", "test/tables/varevo/VarEvo1.schema", tablesAccept},
		{"test/tables/valuedefaults/vd_var_values", "VarDefaults", "vd-var-values", "test/tables/ValueDefaults.schema", tablesAccept},
	} {
		files := []string{c.dir + "/" + c.name + ".schema"}
		if c.wantSchema == "" || c.wantVerdict == tablesPresent {
			files = []string{c.dir + "/" + c.name + ".gotext"}
		}
		slug, schema, verdict, err := tablesCaseFor(c.dir, c.name, files)
		if err != nil {
			t.Errorf("%s %s: %v", c.dir, c.name, err)
			continue
		}
		if slug != c.wantCase || schema != c.wantSchema || verdict != c.wantVerdict {
			t.Errorf("%s %s = %s %s %s, want %s %s %s", c.dir, c.name, slug, schema, verdict, c.wantCase, c.wantSchema, c.wantVerdict)
		}
	}
	if _, _, _, err := tablesCaseFor("test/tables/enumbound", "NewShape", []string{"test/tables/enumbound/NewShape.schema"}); err == nil {
		t.Error("an enum-bound shape with no held verdict was answered")
	}
	if got := tablesSlug("VNEW_array_bounded_grow"); got != "vnew-array-bounded-grow" {
		t.Errorf("tablesSlug = %q", got)
	}
}

// TestTablesGenerationIsJudgedByTheCorpusVerdict: an accepted schema
// that refuses and a refused schema that generates are each a failure.
func TestTablesGenerationIsJudgedByTheCorpusVerdict(t *testing.T) {
	boom := exitedNonZero(t) // the compiler ran and exited non-zero
	for _, c := range []struct {
		verdict string
		err     error
		pass    bool
	}{
		{tablesAccept, nil, true},
		{tablesAccept, boom, false},
		{tablesRefuse, boom, true},
		{tablesRefuse, nil, false},
		{tablesPresent, nil, false},
	} {
		pass, reason := tablesVerdict(c.verdict, c.err, "schema: refused in a table body")
		if pass != c.pass {
			t.Errorf("verdict %s with err %v: pass = %v, want %v", c.verdict, c.err, pass, c.pass)
		}
		if !pass && reason == "" {
			t.Errorf("verdict %s with err %v: a failure with no reason", c.verdict, c.err)
		}
	}
}

// TestATablesCompilerThatDidNotRunFailsEitherVerdict: a compiler that could
// not be started (another build rewriting bin/schema: text file busy, or the
// file gone) refused nothing. A refuse row fails rather than passing on it,
// and both verdicts name why it did not run.
func TestATablesCompilerThatDidNotRunFailsEitherVerdict(t *testing.T) {
	notRun := &os.PathError{Op: "fork/exec", Path: "./bin/schema", Err: syscall.ETXTBSY}
	for _, verdict := range []string{tablesAccept, tablesRefuse} {
		pass, reason := tablesVerdict(verdict, notRun, "")
		if pass || !strings.Contains(reason, "did not run") || !strings.Contains(reason, syscall.ETXTBSY.Error()) {
			t.Errorf("verdict %s with a compiler that did not run: pass = %v, reason %q; want a failure naming %q", verdict, pass, reason, syscall.ETXTBSY.Error())
		}
	}
	if pass, reason := tablesVerdict(tablesAccept, exitedNonZero(t), "schema: refused"); pass || !strings.Contains(reason, "refused a schema the corpus holds as valid") {
		t.Errorf("an accept row whose compiler exited non-zero: pass = %v, reason %q", pass, reason)
	}
	// a compiler a signal ended (a crash, the run's deadline) has no exit
	// status above 0: it refused nothing either
	killed := &exec.ExitError{}
	if killed.ExitCode() > 0 {
		t.Fatalf("an ExitError with no process state has exit code %d", killed.ExitCode())
	}
	for _, verdict := range []string{tablesAccept, tablesRefuse} {
		if pass, reason := tablesVerdict(verdict, killed, ""); pass || !strings.Contains(reason, "did not finish") {
			t.Errorf("verdict %s with a compiler a signal ended: pass = %v, reason %q", verdict, pass, reason)
		}
	}
}

// exitedNonZero is the error of a process that ran and exited with status
// 2: this test binary, handed a flag it does not have.
func exitedNonZero(t *testing.T) error {
	t.Helper()
	err := exec.Command(os.Args[0], "-test.no-such-flag").Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("the test binary with a bad flag: %v, want exit status 2", err)
	}
	return err
}
