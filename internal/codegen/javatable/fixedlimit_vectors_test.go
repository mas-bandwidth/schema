package javatable

import (
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched. Every answer is limits.txt's, which the C++ reference wrote from its
// own reader. This leg's probe prints no duplicate counter and runs no layout
// parse of its own, and it prints a uint32 as Java's signed int, so a value is
// compared modulo 2^32.

// javaLimitValue is one landed value: the VALUE path the probe prints and, for
// a byte of text, the index into the bytes it prints.
type javaLimitValue struct {
	path  string // limits.txt's path
	value string // the probe's VALUE path
	index int    // -1, or the byte of a bytes:<hex> value
}

// javaLimitReason is a refusal name's Java spelling, the enum constant the
// report prints: layout_malformed is layoutMalformed.
func javaLimitReason(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func javaLimitCounters(t *testing.T, file string, v fixedlimit.Vector, r probeRun) {
	t.Helper()
	malformed := 0
	if r.malformed {
		malformed = 1
	}
	for _, c := range []struct {
		name string
		got  int
	}{{"unknown", r.unknown}, {"kind_mismatch", r.kindMismatch}, {"widened", r.widened}, {"clamped", r.clamped}, {"malformed", malformed}} {
		if want := v.Counters[c.name]; int64(c.got) != want {
			t.Errorf("%s: %s=%d, want %d: %+v", file, c.name, c.got, want, r)
		}
	}
}

func javaLimitRun(t *testing.T, row, at, over string, values []javaLimitValue) {
	t.Helper()
	slowtest.Gate(t, "javac and java (it runs the generated NEW reader over the limit vectors)")
	corpus := corpusDir(t)
	a, err := fixedlimit.Read(corpus, at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := fixedlimit.Read(corpus, over)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() || !o.Refused() {
		t.Fatalf("limits.txt: %s must read and %s must refuse: %+v %+v", at, over, a, o)
	}
	_, classes := buildRow(t, t.TempDir(), row, []sideSpec{
		{key: "reads", schema: "VNEW_" + row + ".schema", older: []string{"VOLD_" + row + ".schema"}},
	})
	r := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, at))
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("%s: the at-limit file reads one record: %+v", at, r.n)
	}
	javaLimitCounters(t, at, a, r)
	for _, v := range values {
		want, err := a.Int(v.path)
		if err != nil {
			t.Fatal(err)
		}
		text := r.value[v.value]
		var got int64
		if v.index >= 0 {
			raw, err := hex.DecodeString(strings.TrimPrefix(text, "bytes:"))
			if err != nil || v.index >= len(raw) {
				t.Fatalf("%s: %s is not bytes long enough: %.40q", at, v.value, text)
			}
			got = int64(raw[v.index])
		} else if got, err = strconv.ParseInt(text, 10, 64); err != nil {
			t.Fatalf("%s: %s printed %q", at, v.value, text)
		}
		if uint32(got) != uint32(want) {
			t.Errorf("%s: %s landed %d, want %d", at, v.path, got, want)
		}
	}
	bad := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, over))
	if bad.n >= 0 || !bad.refused || bad.reason != javaLimitReason(o.Reason) {
		t.Fatalf("%s: n=%d refused=%v reason=%s, want refused %s (%s)", over, bad.n, bad.refused, bad.reason, o.Reason, javaLimitReason(o.Reason))
	}
	javaLimitCounters(t, over, o, bad)
}

func TestFixedLimitDepth(t *testing.T) {
	javaLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, []javaLimitValue{
		{"r0.lead", "lead", -1},
		{fixedlimit.DeepPath(), strings.TrimPrefix(fixedlimit.DeepPath(), "r0."), -1},
		{"r0.trail", "trail", -1},
	})
}

func TestFixedLimitRecord(t *testing.T) {
	javaLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, []javaLimitValue{
		{"r0.lead", "lead", -1},
		{"r0.v", "v", -1},
		{"r0.text_length", "textLength", -1},
		{"r0.text[0]", "text", 0},
		{"r0.text[32761]", "text", 32761},
		{"r0.text[65519]", "text", 65519},
		{"r0.trail", "trail", -1},
	})
}
