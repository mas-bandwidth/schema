package gotable

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixedReasonsAreRefuseReasons pins the fold of the fixed form's refusal
// vocabulary into the one refusal enum (docs/SPEC-TABLES.md §6.5, §21.3): for
// every fixed-form refusal TestFixedLoadNamesEveryRefusal drives, the report's
// Reason is the refusal's own name and the report's Err() is the
// TableRefuseReason member that name answers — the named layout/plan member, or
// TableRefuseUnknownForm for a form-byte refusal, which §6.5's strict error
// identity keeps answering unknown_form. errors.Is(err, the member) holds.
func TestFixedReasonsAreRefuseReasons(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import (
	"errors"
	"testing"
)

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func TestReasonsAreMembers(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 2)
	forge := func(edit func(b []byte) []byte) []byte { return edit(append([]byte(nil), file...)) }
	form := func(v byte) []byte { return forge(func(b []byte) []byte { b[0] = v; return b }) }
	cases := []struct {
		what   string
		reason string
		file   []byte
		cap    int
		plan   []TableFixedEntry
		want   TableRefuseReason
	}{
		{"previous_form", "previous_form", form(1), 2, plan, TableRefuseUnknownForm},
		{"message_form_as_file", "message_form_as_file", form(2), 2, plan, TableRefuseUnknownForm},
		{"newer_form", "newer_form", form(6), 2, plan, TableRefuseUnknownForm},
		{"layout_malformed", "layout_malformed", forge(func(b []byte) []byte { b[TableFixedHeaderBytes+4] ^= 1; return b }), 2, plan, TableRefuseLayoutMalformed},
		{"plan_too_large", "plan_too_large", file, 2, plan[:0], TableRefusePlanTooLarge},
		{"batch_too_large", "batch_too_large", file, 1, plan, TableRefuseBatchTooLarge},
		{"no_layout", "no_layout", forge(func(b []byte) []byte { b[len(b)-2*int(PointFixedRecordBytes)] ^= 1; return b }), 2, plan, TableRefuseNoLayout},
		{"truncated empty", "truncated", nil, 2, plan, TableRefuseTruncated},
		{"truncated short header", "truncated", file[:19], 2, plan, TableRefuseTruncated},
		{"truncated ragged", "truncated", append(append([]byte(nil), file...), 0), 2, plan, TableRefuseTruncated},
		{"reserved_not_zero", "reserved_not_zero", forge(func(b []byte) []byte { b[3] = 1; return b }), 2, plan, TableRefuseReservedNotZero},
	}
	for _, c := range cases {
		dst := make([]Point, c.cap)
		r := TableReport{}
		if n := PointFixedLoad(dst, c.file, c.plan, &r); n != -1 {
			t.Fatalf("%s: n=%d, want -1", c.what, n)
		}
		if r.Reason != c.reason {
			t.Errorf("%s: Reason=%q, want %q", c.what, r.Reason, c.reason)
		}
		if err := r.Err(); err == nil || !errors.Is(err, c.want) {
			t.Errorf("%s: Err()=%v, want errors.Is(err, %v)", c.what, err, c.want)
		}
	}
	forged := forge(func(b []byte) []byte { b[TableFixedHashAt] ^= 1; return b })
	var r TableReport
	if n := PointFixedLoad(make([]Point, 2), forged, plan, &r); n != -1 || r.Reason != "layout_unknown" {
		t.Fatalf("layout_unknown: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); err == nil || !errors.Is(err, TableRefuseLayoutUnknown) {
		t.Fatalf("layout_unknown: Err()=%v, want errors.Is(err, TableRefuseLayoutUnknown)", err)
	}
}
`)
}

// TestNoFixedReasonNamesRemain pins the retirement of the fixed form's own
// string vocabulary (docs/SPEC-TABLES.md §6.5, §21.3): the Go surface declares
// TableRefuseReason members and no TableFixedReason identifier. Another
// language's emitter keeps its own spelling, so this scans the Go table
// surface only.
func TestNoFixedReasonNamesRemain(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	scan := func(path string) bool {
		if strings.HasSuffix(path, "_test.go") {
			return false
		}
		slashed := filepath.ToSlash(path)
		return strings.Contains(slashed, "/internal/codegen/gotable/") ||
			strings.Contains(slashed, "/tables/goruntime/") ||
			strings.Contains(slashed, "/internal/tablenames/go.go") ||
			strings.HasSuffix(slashed, "/generated/bench/tables/go") ||
			strings.Contains(slashed, "/generated/bench/tables/go/") ||
			strings.Contains(slashed, "/generated/bench/paired/go/")
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !scan(path) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("TableFixedReason")) {
			t.Errorf("%s still names the retired fixed-form vocabulary", path)
		}
		if bytes.Contains(data, []byte("TableFixedRefusal")) {
			t.Errorf("%s still names the retired TableFixedRefusal type", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
