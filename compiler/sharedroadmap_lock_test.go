// Shared gate S1–S4 for the NEW Fixed Tables work set (docs/roadmap.sexp, node
// "shared"): the compiler-side obligations every language leg stands on,
// counted once outside the language percentages.  One subtest per task id,
// table-driven, t.Parallel() first.  A clause the tree already holds also
// names the test that holds it, so this file is the gate index and the
// clauses it rebuilds are the ones it drives here.
//
// The page sentences each subtest implements:
//
//	docs/SPEC-TABLES.md §2.10          the SCHEMA LOCK
//	docs/SPEC-TABLES.md §3.4           the fixed form's lineage and floor
//	docs/FIXED-FORM-ALGORITHM.md §5.1  BASELINE(old, new), the monotone law
//	docs/FIXED-FORM-ALGORITHM.md §5.2  the lock's lineage= roll-up and floor
//	docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4  --retire is a declaration
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSharedRoadmapLock drives the four shared-gate clauses through the
// production entry points (Compiler.Load with TablesBaseline/SchemaLock,
// UpdateSchemaLock, RetireSchemaLock, Compiler.Generate).  Each clause quotes
// the sentence it implements.
func TestSharedRoadmapLock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id  string
		run func(*testing.T)
	}{
		{id: "shared/S1", run: testSharedS1},
		{id: "shared/S2", run: testSharedS2},
		{id: "shared/S3", run: testSharedS3},
		{id: "shared/S4", run: testSharedS4},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// testSharedS1 asserts shared/S1: "BASELINE(old, new): the monotone law at
// commit, per row, on evaluated values; every FAIL names the table, the
// definition, the rule and both values; the compiler refuses to generate
// against a lock the schema contradicts" (docs/roadmap.sexp shared/S1;
// docs/FIXED-FORM-ALGORITHM.md §5.1; docs/SPEC-TABLES.md §2.10, §18.2).  The
// held clauses are also covered by internal/baseline.TestRefusals and the
// lock-side narrowing cases.
func testSharedS1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Row.schema")
	const wide = "package s1\n\nfixed table Row\n{\n    v int32\n}\n"
	const narrow = "package s1\n\nfixed table Row\n{\n    v int16\n}\n"
	if err := os.WriteFile(path, []byte(wide), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, wide)
	if _, _, err := baseline.Update(u, paths, "initial schema"); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if _, _, err := UpdateSchemaLock(u, paths); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := os.WriteFile(path, []byte(narrow), 0o644); err != nil {
		t.Fatal(err)
	}

	// BASELINE(old, new): the monotone law at commit, naming the table, the
	// definition, the rule and both values.
	bc := New()
	bc.TablesBaseline = true
	if _, err := bc.Load(paths); err == nil {
		t.Fatal("S1: BASELINE must refuse the narrowing at commit")
	} else if !strings.Contains(err.Error(), "Row.v: wire kind 4 -> 3") {
		t.Fatalf("S1: the BASELINE refusal must name table.definition, rule and both values: %v", err)
	}

	// The SCHEMA LOCK refuses the same narrowing by name (§2.10).
	lc := New()
	lc.SchemaLock = true
	if _, err := lc.Load(paths); err == nil {
		t.Fatal("S1: the compiler must refuse a schema the lock contradicts")
	} else {
		for _, part := range []string{"fixed table Row", "field v", "narrowed (int32 -> int16)"} {
			if !strings.Contains(err.Error(), part) {
				t.Fatalf("S1 §2.10: the lock refusal must name the table, the definition and the rule with both values (%q): %v", part, err)
			}
		}
	}
}

// testSharedS2 asserts shared/S2: the lock holds the lineage and the
// `lineage=0x…` roll-up binds the set, so a deleted or reordered line refuses
// by name; the line is one statement made twice (the parse recomputes the wire
// hash and refuses a disagreement) (docs/roadmap.sexp shared/S2;
// docs/FIXED-FORM-ALGORITHM.md §5.2; docs/SPEC-TABLES.md §3.4).
// The hand-edit refusal (one statement made twice) is held by
// internal/lockfile.TestLockLineageHandEditRefused; the roll-up binding is held
// by internal/lockfile.TestLineageSetIsBoundByTheRollup.
func testSharedS2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "T.schema")
	const one = "package s2\n\nfixed table T\n{\n    x int32\n}\n"
	const two = "package s2\n\nfixed table T\n{\n    x int32\n    y int32\n}\n"
	if err := os.WriteFile(path, []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(unitFromSource(t, one), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	u2 := unitFromSource(t, two)
	if _, _, err := UpdateSchemaLock(u2, paths); err != nil {
		t.Fatal(err)
	}

	lockp := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(lockp)
	if err != nil {
		t.Fatal(err)
	}

	// The line is one statement made twice: the parse recomputes the wire hash
	// and refuses a disagreement (docs/roadmap.sexp shared/S2; docs/FIXED-FORM-ALGORITHM.md §5.2).
	// Hand-editing the wire hash on a lineage line causes the parse recomputation to refuse.
	reWire := regexp.MustCompile(`(lineage wire=0x[0-9a-fA-F]{15})[0-9a-fA-F]`)
	match := reWire.FindSubmatchIndex(data)
	if match == nil {
		t.Fatal("S2 setup: expected lineage wire=0x in lock file")
	}
	corruptWire := make([]byte, len(data))
	copy(corruptWire, data)
	if corruptWire[match[3]] == 48 {
		corruptWire[match[3]] = 49
	} else {
		corruptWire[match[3]] = 48
	}
	if err := os.WriteFile(lockp, corruptWire, 0o644); err != nil {
		t.Fatal(err)
	}
	errs := lockfile.Check(u2, paths)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "two halves of this line disagree") {
		t.Fatalf("S2: parse recomputes wire hash and must refuse disagreement (one statement made twice): %v", errs)
	}
	if err := os.WriteFile(lockp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove the oldest lineage line; the recorded roll-up no longer matches
	// the ordered set, which is one statement made twice (docs/FIXED-FORM-ALGORITHM.md §5.2).
	lines := strings.SplitAfter(string(data), "\n")
	removed := false
	var corrupt strings.Builder
	for _, line := range lines {
		if !removed && strings.HasPrefix(strings.TrimSpace(line), "lineage wire=") {
			removed = true
			continue
		}
		corrupt.WriteString(line)
	}
	if !removed || strings.Count(corrupt.String(), "lineage wire=") != 1 {
		t.Fatal("S2 setup: expected two lineage entries and removed the oldest")
	}
	if err := os.WriteFile(lockp, []byte(corrupt.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	errs = lockfile.Check(u2, paths)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "lineage=0x") || !strings.Contains(errs[0].Error(), "roll-up") {
		t.Fatalf("S2: bound by lineage= must refuse a deleted or reordered line by name: %v", errs)
	}
}

// testSharedS3 asserts shared/S3's owed clause: a second retire of the same
// hash is idempotent — it rewrites nothing and leaves the file byte-identical
// (docs/roadmap.sexp shared/S3; docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4).
// The --retire/--reason clauses and the CLI's "already retired" path are held
// by cmd/schema.TestSharedS3Gate.
func testSharedS3(t *testing.T) {
	dir, paths, w, olderHash := sharedLockWiden(t)
	path := filepath.Join(dir, lockfile.FileName)
	target := fmt.Sprintf("Config@0x%016x", olderHash)
	const reason = "the v1 client is gone"
	if _, rewrote, err := RetireSchemaLock(w, paths, target, reason); err != nil || !rewrote {
		t.Fatalf("S3: the first retire must rewrite the file: rewrote=%v err=%v", rewrote, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := RetireSchemaLock(w, paths, target, reason); err != nil {
		t.Fatalf("S3: the second retire is idempotent and must not fail: %v", err)
	} else if rewrote {
		t.Fatal("S3: the second retire of the same hash must NOT rewrite the file (idempotent)")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("S3: the lock file must be byte-identical after the idempotent second retire")
	}
}

// testSharedS4 asserts shared/S4: COMPILE reads lockfile.Lineage and
// lockfile.Floor — the older layout's hash reaches the emitted source, and the
// emitted floor moves when the operator retires an entry — not the filename
// convention and not a hard-coded floor (docs/roadmap.sexp shared/S4;
// docs/FIXED-FORM-ALGORITHM.md §5.2).  The "not the filename convention" and
// "not a hard-coded floor" clauses are held by TestSharedS4Gate.
func testSharedS4(t *testing.T) {
	_, paths, w, olderHash := sharedLockWiden(t)
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "cpp", nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body := joined(files)
	if !strings.Contains(body, fmt.Sprintf("0x%016x", olderHash)) {
		t.Fatalf("S4: COMPILE reads lockfile.Lineage — the older layout hash 0x%016x is not in the emitted source", olderHash)
	}
	if _, _, err := RetireSchemaLock(w, paths, fmt.Sprintf("Config@0x%016x", olderHash), "S4 gate retirement"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	u2, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err = c.Generate(u2, "cpp", nil)
	if err != nil {
		t.Fatalf("generate after retire: %v", err)
	}
	body = joined(files)
	m := regexp.MustCompile(`ConfigFixedFloor\s*=\s*(\d+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("S4: COMPILE reads lockfile.Floor — no ConfigFixedFloor in the emitted source")
	}
	if m[1] != "1" {
		t.Fatalf("S4: the emitted floor is %s, want 1 — the floor is the lock's, derived from its retired mark, not hard-coded", m[1])
	}
}

// sharedLockWiden builds the lock-widen-lock fixture shared by S3 and S4 and
// returns the directory, the gathered paths, the widened unit, and the older
// layout's wire hash (docs/FIXED-FORM-ALGORITHM.md §5.2).
func sharedLockWiden(t *testing.T) (dir string, paths []string, w *ir.Unit, olderHash uint64) {
	t.Helper()
	dir = t.TempDir()
	path := filepath.Join(dir, "Config.schema")
	const first = "package s3gate\n\nfixed table Config\n{\n    version int32\n    name    string(64)\n}\n"
	const widened = "package s3gate\n\nfixed table Config\n{\n    version int32\n    name    string(64)\n    flags   uint32\n}\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	paths, err = GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Config"]
	if st == nil {
		t.Fatal("fixture must declare Config")
	}
	olderHash = ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
	if _, _, err := UpdateSchemaLock(u, paths); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if err := os.WriteFile(path, []byte(widened), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err = c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st = w.Tables["Config"]
	if st == nil {
		t.Fatal("fixture must declare Config")
	}
	current := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
	if current == olderHash {
		t.Fatal("the widening must change the layout hash")
	}
	if _, _, err := UpdateSchemaLock(w, paths); err != nil {
		t.Fatalf("second lock: %v", err)
	}
	return dir, paths, w, olderHash
}
