package fuzz_test

// tablediff_test.go is the differential half of the random-schema harness
// (card schema-go-fuzz-random-table-differential). For each seed it builds a
// random VALID unit (tablegen_test.go), generates the Go leg with
// compiler.New().Generate(u, "go", nil), writes a probe beside it and runs the
// probe through the house runGeneratedUnit pattern under -race. The probe
// drives 24 values through every read and write path, damages each wire, plants
// hostile JSON and keeps a divergence list instead of stopping at the first
// one. This file then classifies those divergences against the expected-class
// table and runs the compiler's engine (tablepack.UnpackOneFile/Pack) in both
// directions.
//
// A divergence is a finding unless an open card already owns its class; the
// owning card's id is printed with every instance the harness suppresses.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// expectedClass is one class of divergence an open card already owns. The
// harness reports it with the card id and does not fail on it.
type expectedClass struct {
	id    string
	why   string
	match func(u *genUnit, line string) bool
}

// The ids below were each looked up with `nova-sprint card <id>` on 2026-10-18
// (verifiedClassCards in tablediff_classes_test.go pins that list): a class
// whose owner cannot be found is not owed, it is a finding.
var expectedClasses = []expectedClass{
	{
		id:  "schema-tabletext-timestamp-text",
		why: "the engine's text walk spells a timestamp differently from the Go leg's RFC 3339 text (docs/SPEC-TABLES.md §2.11, §16); filed only when every differing leaf is the same instant in two spellings",
		match: func(u *genUnit, line string) bool {
			if !u.hasTimestamp() || !strings.HasPrefix(line, "engine ") {
				return false
			}
			parts := strings.Fields(line)
			if len(parts) != 3 {
				return false
			}
			legRaw, err1 := base64.StdEncoding.DecodeString(parts[1])
			engRaw, err2 := base64.StdEncoding.DecodeString(parts[2])
			if err1 != nil || err2 != nil {
				return false
			}
			eq, tsOnly := classifyDiff(u, legRaw, engRaw)
			return !eq && tsOnly
		},
	},
	{
		id:  "schema-go-plain-float-special-values",
		why: "a plain (non-compressed) float's -0 is written as 0 by one path, so the two texts differ by whole -0 against 0 number tokens and nothing else (docs/SPEC-TABLES.md §3.3)",
		match: func(u *genUnit, line string) bool {
			return u.hasFloat() && onlyNegativeZero(line)
		},
	},
}

func (u *genUnit) hasTimestamp() bool {
	return u.holds(gTimestamp)
}

func (u *genUnit) hasFloat() bool {
	return u.holds(gFloat32) || u.holds(gFloat64)
}

// holds reports whether any field reachable from Root (through elements, map
// keys and values, inner tables by value, pointer or optional, and union arms)
// is of kind k.
func (u *genUnit) holds(k genKind) bool {
	seen := map[string]bool{}
	var field func(f *genField) bool
	var table func(t *genTable) bool
	table = func(t *genTable) bool {
		if t == nil || seen["t:"+t.Name] {
			return false
		}
		seen["t:"+t.Name] = true
		for _, f := range t.Fields {
			if field(f) {
				return true
			}
		}
		return false
	}
	field = func(f *genField) bool {
		if f == nil {
			return false
		}
		if f.Kind == k || field(f.Elem) || field(f.MapKey) {
			return true
		}
		if f.NestedName != "" {
			for _, t := range u.Inner {
				if t.Name == f.NestedName && table(t) {
					return true
				}
			}
		}
		if f.UnionName != "" && !seen["u:"+f.UnionName] {
			seen["u:"+f.UnionName] = true
			for _, un := range u.Unions {
				if un.Name != f.UnionName {
					continue
				}
				for _, arm := range un.Arms {
					if field(arm) {
						return true
					}
				}
			}
		}
		return false
	}
	return table(u.Root)
}

// onlyNegativeZero reports whether the two base64 JSON payloads on a divergence
// line differ ONLY by whole number tokens -0 against 0, with at least one such
// pair. Both texts are walked token by token with json.Decoder.UseNumber, and
// the raw bytes of every token (with the separators and whitespace before it)
// must be identical except at a -0/0 pair; the bytes after the last token must
// be identical too. So a 64-bit integer off by one, a float that lost its sign
// or exponent, reordered keys, 1e2 against 100, an escape against the literal
// character, a trailing space, and two identical texts are all NOT masked.
func onlyNegativeZero(line string) bool {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return false
	}
	a, err1 := base64.StdEncoding.DecodeString(parts[1])
	b, err2 := base64.StdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	return onlyNegativeZeroText(a, b)
}

func onlyNegativeZeroText(a, b []byte) bool {
	side := 0 // +1: the -0 is in a, -1: in b; every pair must agree
	diffs, ok := leafDiffWalk(a, b, func(ta, tb json.Token, _ []string) bool {
		na, okA := ta.(json.Number)
		nb, okB := tb.(json.Number)
		if !okA || !okB || !negZeroPair(string(na), string(nb)) {
			return false
		}
		s := -1
		if strings.HasPrefix(string(na), "-") {
			s = 1
		}
		if side == 0 {
			side = s
		}
		return side == s
	})
	return ok && diffs > 0
}

// negZeroPair reports whether x and y are the same zero spelling, one of them
// with a leading minus sign: "-0" against "0", "-0.0" against "0.0".
func negZeroPair(x, y string) bool {
	if len(x) < len(y) {
		x, y = y, x
	}
	if x != "-"+y || strings.HasPrefix(y, "-") {
		return false
	}
	f, err := strconv.ParseFloat(y, 64)
	return err == nil && f == 0
}

// leafDiffWalk walks two JSON texts token by token (json.Decoder.UseNumber)
// and compares the RAW bytes of every token together with the separators and
// whitespace before it. Where the raw bytes differ, the separators must still
// be identical, the token must be a VALUE (an object key that differs is never
// allowed, whatever it spells), both tokens must be leaves (string or number),
// and allow must accept them given the value's path: the object keys from the
// root down to it (array levels add nothing). Each text must be exactly one
// JSON value, and the bytes after it must be identical. It returns the number
// of differing leaves, and ok false on any other difference.
func leafDiffWalk(a, b []byte, allow func(ta, tb json.Token, path []string) bool) (diffs int, ok bool) {
	da := json.NewDecoder(bytes.NewReader(a))
	db := json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	type frame struct {
		obj     bool   // an object (keys alternate with values), else an array
		wantKey bool   // the next token in this object is a key
		key     string // the key of the value being read
	}
	var stack []frame
	var offA, offB int64
	for {
		ta, ea := da.Token()
		tb, eb := db.Token()
		if ea != nil || eb != nil {
			return 0, false // a decode error, or a text that ended early
		}
		rawA := a[offA:da.InputOffset()]
		rawB := b[offB:db.InputOffset()]
		offA, offB = da.InputOffset(), db.InputOffset()
		d, isDelim := ta.(json.Delim)
		closing := isDelim && (d == '}' || d == ']')
		isKey := !closing && len(stack) > 0 && stack[len(stack)-1].obj && stack[len(stack)-1].wantKey
		if !bytes.Equal(rawA, rawB) {
			if isKey || !isLeaf(ta) || !isLeaf(tb) || !bytes.Equal(separators(rawA), separators(rawB)) {
				return 0, false
			}
			var path []string
			for _, f := range stack {
				if f.obj {
					path = append(path, f.key)
				}
			}
			if !allow(ta, tb, path) {
				return 0, false
			}
			diffs++
		}
		switch {
		case isKey:
			k, _ := ta.(string)
			stack[len(stack)-1].key = k
			stack[len(stack)-1].wantKey = false
		case closing:
			stack = stack[:len(stack)-1]
		case isDelim:
			stack = append(stack, frame{obj: d == '{', wantKey: true})
			continue // the value ends at its closing delimiter
		}
		if len(stack) == 0 {
			break
		}
		if !isKey && stack[len(stack)-1].obj {
			stack[len(stack)-1].wantKey = true // a value ended: a key comes next
		}
	}
	if _, err := da.Token(); err != io.EOF {
		return 0, false
	}
	if _, err := db.Token(); err != io.EOF {
		return 0, false
	}
	return diffs, bytes.Equal(a[offA:], b[offB:])
}

func isLeaf(t json.Token) bool {
	switch t.(type) {
	case string, json.Number:
		return true
	}
	return false
}

// separators returns the run of whitespace, commas and colons that opens a raw
// token slice.
func separators(raw []byte) []byte {
	i := 0
	for i < len(raw) && strings.IndexByte(" \t\r\n,:", raw[i]) >= 0 {
		i++
	}
	return raw[:i]
}

// classifyDiff compares the leg's text with the engine's for unit u. eq
// reports whether they are byte-identical; tsOnly reports whether they differ
// and EVERY difference is a value leaf at a path u types as timestamp
// (kindAt) that is a timestamp spelling (isTimestampSpelling): the same
// instant as RFC 3339 text on one side and its UTC nanosecond count, or
// another RFC 3339 spelling, on the other. Any other difference anywhere — a
// wrong bool, a wrong int, a changed key, an order, an escape, whitespace,
// trailing bytes, a timestamp-looking string in a string field — makes tsOnly
// false, so a timestamp crossing never masks a second bug on the same line
// (leafDiffWalk).
func classifyDiff(u *genUnit, leg, eng []byte) (eq, tsOnly bool) {
	if bytes.Equal(leg, eng) {
		return true, false
	}
	diffs, ok := leafDiffWalk(leg, eng, func(ta, tb json.Token, path []string) bool {
		k, found := u.kindAt(path)
		return found && k == gTimestamp && isTimestampSpelling(ta, tb)
	})
	return false, ok && diffs > 0
}

// kindAt returns the scalar kind of the value at path (object keys from the
// root, as leafDiffWalk gives them) in u's JSON form (tablegen_test.go
// randomJSON): a table's keys are its field names, a map's keys are any key,
// a union is an object of one arm, and arrays, optionals and pointers add no
// key. found is false for a path the schema does not have.
func (u *genUnit) kindAt(path []string) (genKind, bool) {
	var table func(t *genTable, rest []string) (genKind, bool)
	var field func(f *genField, rest []string) (genKind, bool)
	table = func(t *genTable, rest []string) (genKind, bool) {
		if t == nil || len(rest) == 0 {
			return 0, false
		}
		for _, f := range t.Fields {
			if f.Name == rest[0] {
				return field(f, rest[1:])
			}
		}
		return 0, false
	}
	field = func(f *genField, rest []string) (genKind, bool) {
		if f == nil {
			return 0, false
		}
		switch f.Kind {
		case gScalarArray, gFixedArray, gOpenArray, gOptScalar, gPointer:
			return field(f.Elem, rest)
		case gNested:
			return table(u.innerTable(f.NestedName), rest)
		case gMap:
			if len(rest) == 0 {
				return 0, false
			}
			return field(f.Elem, rest[1:])
		case gUnion:
			un := u.union(f.UnionName)
			if un == nil || len(rest) == 0 {
				return 0, false
			}
			for _, arm := range un.Arms {
				if arm.Name == rest[0] {
					return field(arm, rest[1:])
				}
			}
			return 0, false
		}
		if len(rest) != 0 {
			return 0, false
		}
		return f.Kind, true
	}
	return table(u.Root, path)
}

// isTimestampSpelling reports whether two differing leaves name the same
// instant: an RFC 3339 string (docs/SPEC-TABLES.md §2.11) against its UTC
// nanosecond count, or two RFC 3339 strings of the same instant.
func isTimestampSpelling(x, y json.Token) bool {
	nanos := func(v any) (int64, bool) {
		switch s := v.(type) {
		case string:
			tm, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return 0, false
			}
			return tm.UnixNano(), true
		case json.Number:
			n, err := strconv.ParseInt(string(s), 10, 64)
			return n, err == nil
		}
		return 0, false
	}
	_, xs := x.(string)
	_, ys := y.(string)
	if !xs && !ys {
		return false // two counts that differ are a wrong value, not a spelling
	}
	nx, ok1 := nanos(x)
	ny, ok2 := nanos(y)
	return ok1 && ok2 && nx == ny
}

func tableUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("probe.schema", []byte(src))
	if len(perrs) > 0 || ast == nil {
		t.Fatalf("parse: %v", perrs)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{Path: "probe.schema", Name: "probe.schema", Base: "probe", Bytes: []byte(src), AST: ast}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return unit
}

// runGeneratedTableUnit mirrors the house runGeneratedUnit pattern
// (internal/codegen/gotable/values_test.go:34) so the differential probe runs in
// a t.TempDir against the serialize.go replace line.
func runGeneratedTableUnit(t *testing.T, u *ir.Unit, testSource string, env map[string]string, flags ...string) ([]byte, error) {
	t.Helper()
	files, err := compiler.New().Generate(u, "go", nil)
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../serialize.go")
	if err != nil {
		return nil, err
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return nil, err
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	args := append([]string{"test", "-count=1"}, flags...)
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.CombinedOutput()
}

var tableDifferentialProbe = `package probe

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func record(div *[]string, format string, args ...any) {
	*div = append(*div, fmt.Sprintf(format, args...))
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	n, _ := strconv.Atoi(os.Getenv("SCHEMA_TABLE_DIFF_N"))
	sabotage := os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE") == "1"
	var div []string
	plants := []string{
		"null", "-0", "1e999", "NaN", "\"\\ud800\"", "\ufeff{}", "{} {}", "{\"a\":1,\"a\":2}",
		"{" + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + "}",
	}
	for i := 0; i < n; i++ {
		jb, err := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var v Root
		var report TableReport
		RootReset(&v)
		if !RootFromJson(&v, jb, &report) {
			record(&div, "fromjson %d %s", i, b64(jb))
			continue
		}
		tj := make([]byte, RootToJsonMeasure(&v))
		if RootToJson(&v, tj) != int64(len(tj)) {
			record(&div, "tojson %d", i)
		}
		if sabotage && i == 0 {
			tj = append(tj, ' ')
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".json", tj, 0o600); err != nil {
			t.Fatal(err)
		}
		wire := make([]byte, RootMeasure(&v))
		if RootSave(&v, wire) != int64(len(wire)) {
			record(&div, "save %d", i)
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".wire", wire, 0o600); err != nil {
			t.Fatal(err)
		}
		var v2 Root
		RootReset(&v2)
		if RootFromJson(&v2, tj, &report) {
			tj2 := make([]byte, RootToJsonMeasure(&v2))
			if RootToJson(&v2, tj2) != int64(len(tj2)) || string(tj2) != string(tj) {
				record(&div, "fromjsontojson %d %s %s", i, b64(tj), b64(tj2))
			}
			w2 := make([]byte, RootMeasure(&v2))
			if RootSave(&v2, w2) != int64(len(w2)) || string(w2) != string(wire) {
				record(&div, "fromjsontojsonresave %d", i)
			}
		} else {
			record(&div, "fromjsontojsonrefuse %d %s", i, b64(tj))
		}
		got, err := RootReadStrict(wire)
		if err != nil || got == nil {
			record(&div, "strictrefuse %d", i)
		} else {
			gj := make([]byte, RootToJsonMeasure(got))
			if RootToJson(got, gj) != int64(len(gj)) {
				record(&div, "stricttojson %d", i)
			}
			if string(gj) != string(tj) {
				record(&div, "roundtrip %s %s", b64(tj), b64(gj))
			}
			gw := make([]byte, RootMeasure(got))
			if RootSave(got, gw) != int64(len(gw)) || string(gw) != string(wire) {
				record(&div, "strictresave %d", i)
			}
		}
		st := base64.StdEncoding.EncodeToString(wire)
		if stxt, err := RootSaveText(&v); err != nil || stxt != st {
			record(&div, "savetext %s %s", st, stxt)
		}
		lt, err := RootLoadText(st, &report)
		if err != nil {
			record(&div, "loadtext %s %s", b64([]byte(st)), b64([]byte("error")))
		} else {
			lw := make([]byte, RootMeasure(lt))
			if RootSave(lt, lw) != int64(len(lw)) {
				record(&div, "loadtextsave %d", i)
			}
			if base64.StdEncoding.EncodeToString(lw) != st {
				record(&div, "loadtext %s %s", b64([]byte(st)), b64([]byte(base64.StdEncoding.EncodeToString(lw))))
			}
		}
		var loaded Root
		RootReset(&loaded)
		if !RootLoad(&loaded, wire, &report) {
			record(&div, "loadrefuse %d", i)
		} else {
			lj := make([]byte, RootToJsonMeasure(&loaded))
			if RootToJson(&loaded, lj) != int64(len(lj)) {
				record(&div, "loadtojson %d", i)
			} else if string(lj) != string(tj) {
				record(&div, "loadroundtrip %s %s", b64(tj), b64(lj))
			}
			lw := make([]byte, RootMeasure(&loaded))
			if RootSave(&loaded, lw) != int64(len(lw)) || string(lw) != string(wire) {
				record(&div, "loadresave %d", i)
			}
		}
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = RootReadStrict(wire)
			}()
		}
		wg.Wait()
		// damage: every truncation, every single-byte xor, then random splices/inserts
		var ops [][]byte
		for cut := 0; cut < len(wire); cut++ {
			ops = append(ops, append([]byte{}, wire[:cut]...))
		}
		for idx := 0; idx < len(wire); idx++ {
			for _, m := range []byte{0x01, 0x80, 0xff} {
				cp := append([]byte{}, wire...)
				cp[idx] ^= m
				ops = append(ops, cp)
			}
		}
		dr := rand.New(rand.NewSource(1))
		for k := 0; k < 100; k++ {
			cp := append([]byte{}, wire...)
			if len(wire) > 1 && dr.Intn(2) == 0 {
				a := dr.Intn(len(wire))
				b := a + dr.Intn(len(wire)-a)
				cp = append(cp[:a], cp[b:]...)
			} else if len(wire) > 1 {
				at := dr.Intn(len(wire))
				cp = append(cp[:at], append([]byte{byte(dr.Intn(256))}, cp[at:]...)...)
			}
			ops = append(ops, cp)
		}
		for _, d := range ops {
			func() {
				defer func() {
					if r := recover(); r != nil {
						record(&div, "damagepanic %v", r)
					}
				}()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				var dv Root
				var dr TableReport
				RootReset(&dv)
				_ = RootLoad(&dv, d, &dr)
				_, _ = RootMeasureReason(&dv)
				_, _ = RootReadStrict(d)
				_, _ = RootLoadText(base64.StdEncoding.EncodeToString(d), &dr)
				runtime.ReadMemStats(&after)
				if len(d) < 4096 && after.TotalAlloc-before.TotalAlloc > 4<<20 {
					record(&div, "damagealloc %d", after.TotalAlloc-before.TotalAlloc)
				}
			}()
		}
		// hostile JSON plants, with the whole call under recover
		for _, plant := range plants {
			func() {
				defer func() {
					if r := recover(); r != nil {
						record(&div, "plantpanic %v", r)
					}
				}()
				var pv Root
				var pr TableReport
				RootReset(&pv)
				_ = RootFromJson(&pv, []byte(plant), &pr)
			}()
		}
	}
	if len(div) > 0 {
		_ = os.WriteFile(dir+"/divergences.txt", []byte(strings.Join(div, "\n")+"\n"), 0o600)
		t.Fatalf("%d divergence(s)", len(div))
	}
}
`

// runSeed generates, compiles and runs one seed, then classifies what the probe
// reported and compares the engine in both directions. It returns the
// unexpected divergences (which fail the seed) and a log of expected ones.
func runSeed(t *testing.T, r *rand.Rand, seed int, sabotage bool) (unexpected []string, expected []string) {
	t.Helper()
	src, u := randomSchema(r)
	unit := tableUnit(t, src)
	dir := t.TempDir()
	for i := 0; i < 24; i++ {
		j := randomJSON(r, u, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)), []byte(j), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir, "SCHEMA_TABLE_DIFF_N": "24"}
	if sabotage {
		env["SCHEMA_TABLE_DIFF_SABOTAGE"] = "1"
	}
	out, err := runGeneratedTableUnit(t, unit, tableDifferentialProbe, env, "-race")
	divBytes, _ := os.ReadFile(filepath.Join(dir, "divergences.txt"))
	skip := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(divBytes)), "\n") {
		if strings.HasPrefix(line, "fromjson ") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if n, err := strconv.Atoi(f[1]); err == nil {
					skip[n] = true
				}
			}
		}
		if line == "" {
			continue
		}
		owned := false
		for _, c := range expectedClasses {
			if c.match(u, line) {
				expected = append(expected, fmt.Sprintf("seed %d: %s (%s): %s", seed, c.id, c.why, shorten(line)))
				owned = true
				break
			}
		}
		if !owned {
			unexpected = append(unexpected, fmt.Sprintf("seed %d: unexpected divergence: %s", seed, shorten(line)))
		}
	}
	if err != nil && len(divBytes) == 0 {
		unexpected = append(unexpected, fmt.Sprintf("seed %d: probe failed with no divergence report: %v\n%s", seed, err, out))
		return unexpected, expected
	}
	if len(unexpected) > 0 {
		return unexpected, expected
	}
	m := tabletext.NewModel(unit)
	for i := 0; i < 24; i++ {
		if skip[i] {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read wire: %v", seed, i, err))
			continue
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read text: %v", seed, i, err))
			continue
		}
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile: %v", seed, i, err))
			continue
		}
		if report != (tabletext.Report{}) {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile report not clean: %+v", seed, i, report))
			continue
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read engine text: %v", seed, i, err))
			continue
		}
		pack := func(legJSON []byte) ([]byte, tabletext.Report, error) {
			d2 := t.TempDir()
			if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
				return nil, tabletext.Report{}, err
			}
			wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
			return wireBack, report2, err
		}
		un, ex := engineCheck(u, seed, i, wire, legJSON, engineJSON, pack)
		unexpected = append(unexpected, un...)
		expected = append(expected, ex...)
	}
	return unexpected, expected
}

// engineCheck compares one instance's engine text (tablepack.UnpackOneFile of
// the leg's wire) with the leg's own text, then runs the other direction: pack
// (tablepack.Pack of the leg's text) must give the leg's wire back. A text
// divergence an open card owns is reported, not failed.
func engineCheck(u *genUnit, seed, i int, wire, legJSON, engineJSON []byte, pack func(legJSON []byte) ([]byte, tabletext.Report, error)) (unexpected, expected []string) {
	if !bytes.Equal(engineJSON, legJSON) {
		line := "engine " + base64.StdEncoding.EncodeToString(legJSON) + " " + base64.StdEncoding.EncodeToString(engineJSON)
		owned := false
		for _, c := range expectedClasses {
			if c.match(u, line) {
				expected = append(expected, fmt.Sprintf("seed %d inst %d: %s: engine text differs from leg text", seed, i, c.id))
				owned = true
				break
			}
		}
		if !owned {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: unexpected engine/leg text divergence\nleg: %s\nengine: %s", seed, i, legJSON, engineJSON))
		}
		// No early return: an owed text class never skips the Pack
		// direction below, which must still give the leg's wire back from
		// the leg's text.
	}
	wireBack, report2, err := pack(legJSON)
	if err != nil {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack: %v", seed, i, err))
		return unexpected, expected
	}
	if report2 != (tabletext.Report{}) {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack report not clean: %+v", seed, i, report2))
		return unexpected, expected
	}
	if !bytes.Equal(wireBack, wire) {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: engine wire differs from leg wire", seed, i))
	}
	return unexpected, expected
}

func shorten(s string) string {
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}

// tableDiffOptIn skips the two tests below unless SCHEMA_TABLE_DIFF=1, which
// only `make fuzz-tables-diff` (make/go.mk) sets. They compile and run a
// generated unit per seed under -race, so they are a nightly campaign
// (certify.yml), never part of ci-fast or ci-full: ci-full's SCHEMA_SLOW=1
// `go test` over ./internal/fuzz still runs the crasher corpus and the property
// and oracle tests, and skips only these two here.
func tableDiffOptIn(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units) and the engine")
	if !tableDiffOptedIn(os.Getenv) {
		t.Skip("SCHEMA_TABLE_DIFF: the random-schema table differential is a nightly campaign; run it with make fuzz-tables-diff (it sets SCHEMA_TABLE_DIFF=1)")
	}
}

// tableDiffOptedIn reports whether the two tests below run in this process:
// only when SCHEMA_TABLE_DIFF=1, never on SCHEMA_SLOW=1 alone.
func tableDiffOptedIn(getenv func(string) string) bool {
	return getenv("SCHEMA_TABLE_DIFF") == "1"
}

// TestRandomTableDifferential is the gate: seeds SCHEMA_TABLE_DIFF_FROM..TO
// (default 0..4), each generated, compiled and run under -race.
func TestRandomTableDifferential(t *testing.T) {
	t.Parallel()
	tableDiffOptIn(t)
	from, to := 0, 4
	if v := os.Getenv("SCHEMA_TABLE_DIFF_FROM"); v != "" {
		from, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SCHEMA_TABLE_DIFF_TO"); v != "" {
		to, _ = strconv.Atoi(v)
	}
	for _, c := range expectedClasses {
		t.Logf("expected class owned by %s: %s", c.id, c.why)
	}
	for seed := from; seed <= to; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			unexpected, expected := runSeed(t, rand.New(rand.NewSource(int64(seed))), seed, false)
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			for _, e := range unexpected {
				t.Errorf("%s", e)
			}
		})
	}
}

// TestRandomTableDifferentialNegativeControl flips one probe comparison and
// requires the gate to go red on an unexpected divergence, not on a compile
// error.
func TestRandomTableDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	tableDiffOptIn(t)
	unexpected, _ := runSeed(t, rand.New(rand.NewSource(0)), 0, true)
	if len(unexpected) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: a flipped probe comparison left the gate green")
	}
	if !strings.Contains(strings.Join(unexpected, "\n"), "unexpected divergence") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on an unexpected divergence: %v", unexpected)
	}
}

var _ = math.MaxInt8
