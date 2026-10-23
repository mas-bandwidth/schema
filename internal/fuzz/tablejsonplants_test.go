package fuzz_test

// tablejsonplants_test.go is the JSON-plant companion to tablediff_test.go
// (card schema-go-fuzz-random-table-differential-json-plants). For each seed
// from randomSchema it generates the Go leg with compiler.New().Generate(u,
// "go", nil), builds a valid randomJSON text of the unit, plants each of the
// nine hostile or boundary JSON cases into that text, and runs the generated
// unit's RootFromJson under recover and -race.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// plantSpec is one planted JSON input for a generated Root table.
// mode is "must_refuse" (RootFromJson must return false or report.Err() != nil),
// "must_accept" (RootFromJson must return true with report.Err() == nil), or
// "either" (either refused by name via report.Err() != nil, or accepted cleanly).
type plantSpec struct {
	name string
	mode string
	text string
}

// validUnitJSON returns a valid JSON text for u from randomJSON(r, u, 1),
// rewriting any raw integer timestamp leaf from tablegen_test.go into an
// RFC 3339 string so RootFromJson accepts the base text cleanly before any
// plant is applied.
func validUnitJSON(r *rand.Rand, u *genUnit) string {
	raw := randomJSON(r, u, 1)
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return "{}"
	}
	fixTimestampsInTable(u, u.Root, obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(out)
}

func fixTimestampsInTable(u *genUnit, tbl *genTable, obj map[string]any) {
	if tbl == nil || obj == nil {
		return
	}
	for _, f := range tbl.Fields {
		v, ok := obj[f.Name]
		if !ok || v == nil {
			continue
		}
		obj[f.Name] = fixTimestampsInField(u, f, v)
	}
}

func fixTimestampsInField(u *genUnit, f *genField, v any) any {
	if f == nil || v == nil {
		return v
	}
	switch f.Kind {
	case gTimestamp:
		return "1970-01-01T00:00:00Z"
	case gOptScalar, gPointer:
		return fixTimestampsInField(u, f.Elem, v)
	case gScalarArray, gFixedArray, gOpenArray:
		if arr, ok := v.([]any); ok {
			for i := range arr {
				arr[i] = fixTimestampsInField(u, f.Elem, arr[i])
			}
			return arr
		}
	case gNested:
		if m, ok := v.(map[string]any); ok {
			fixTimestampsInTable(u, u.innerTable(f.NestedName), m)
			return m
		}
	case gUnion:
		un := u.union(f.UnionName)
		if m, ok := v.(map[string]any); ok && un != nil {
			for _, arm := range un.Arms {
				if av, exists := m[arm.Name]; exists {
					m[arm.Name] = fixTimestampsInField(u, arm, av)
				}
			}
			return m
		}
	}
	return v
}

// withFieldReplaced returns baseJSON (a JSON object) with field fname's raw
// JSON value replaced by rawVal.
func withFieldReplaced(baseJSON, fname, rawVal string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(baseJSON), &raw); err != nil {
		return fmt.Sprintf("{%q:%s}", fname, rawVal)
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	wrote := false
	for k, v := range raw {
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%q:", k)
		if k == fname {
			b.WriteString(rawVal)
			wrote = true
		} else {
			b.Write(v)
		}
	}
	if !wrote {
		if !first {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%s", fname, rawVal)
	}
	b.WriteByte('}')
	return b.String()
}

// withDuplicateKey returns baseJSON with its first field key repeated.
func withDuplicateKey(baseJSON, fname string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(baseJSON), &raw); err != nil {
		return `{"f0":0,"f0":0}`
	}
	val, ok := raw[fname]
	if !ok {
		val = json.RawMessage("0")
	}
	var b strings.Builder
	b.WriteByte('{')
	fmt.Fprintf(&b, "%q:%s,%q:%s", fname, string(val), fname, string(val))
	for k, v := range raw {
		if k == fname {
			continue
		}
		fmt.Fprintf(&b, ",%q:%s", k, string(v))
	}
	b.WriteByte('}')
	return b.String()
}

// scalarNullPlant returns a JSON text derived from baseJSON where a non-optional
// scalar position holds null, so RootFromJson must refuse it with kind_mismatch.
func scalarNullPlant(u *genUnit, baseJSON string) string {
	for _, f := range u.Root.Fields {
		switch f.Kind {
		case gInt, gUint, gFloat32, gFloat64, gBool, gEnum, gString, gBytes, gTimestamp:
			return withFieldReplaced(baseJSON, f.Name, "null")
		case gScalarArray, gFixedArray, gOpenArray:
			return withFieldReplaced(baseJSON, f.Name, "[null]")
		case gUnion:
			return withFieldReplaced(baseJSON, f.Name, `{"a":null}`)
		case gNested:
			it := u.innerTable(f.NestedName)
			if it != nil && len(it.Fields) > 0 {
				g := it.Fields[0]
				if g.Kind == gScalarArray || g.Kind == gFixedArray || g.Kind == gOpenArray {
					return withFieldReplaced(baseJSON, f.Name, fmt.Sprintf("{%q:[null]}", g.Name))
				}
				if g.Kind != gOptScalar {
					return withFieldReplaced(baseJSON, f.Name, fmt.Sprintf("{%q:null}", g.Name))
				}
			}
		}
	}
	// Every field on Root was gOptScalar: plant null on f0's inner scalar if ?Inner,
	// or on f0 itself.
	f0 := u.Root.Fields[0]
	if f0.Kind == gOptScalar && f0.Elem != nil && f0.Elem.Kind == gNested {
		it := u.innerTable(f0.Elem.NestedName)
		if it != nil && len(it.Fields) > 0 && it.Fields[0].Kind != gOptScalar {
			g := it.Fields[0]
			if g.Kind == gScalarArray || g.Kind == gFixedArray || g.Kind == gOpenArray {
				return withFieldReplaced(baseJSON, f0.Name, fmt.Sprintf("{%q:[null]}", g.Name))
			}
			return withFieldReplaced(baseJSON, f0.Name, fmt.Sprintf("{%q:null}", g.Name))
		}
	}
	return withFieldReplaced(baseJSON, f0.Name, "null")
}

// tableNullPlants returns JSON texts derived from baseJSON where a table
// position (the root table itself, and any non-optional nested table field)
// holds null.
func tableNullPlants(u *genUnit, baseJSON string) []plantSpec {
	out := []plantSpec{
		{name: "null_table_root", mode: "must_refuse", text: "null"},
	}
	for _, f := range u.Root.Fields {
		if f.Kind == gNested {
			out = append(out, plantSpec{
				name: "null_table_field_" + f.Name,
				mode: "must_refuse",
				text: withFieldReplaced(baseJSON, f.Name, "null"),
			})
			break
		}
	}
	return out
}

// buildPlantSpecs builds the nine plant classes into baseJSON for unit u.
// Plant (9) (10^6 nested arrays) is expanded inside the probe process so we do
// not write a 2 MB manifest line.
func buildPlantSpecs(u *genUnit, baseJSON string, sabotage bool) []plantSpec {
	f0 := u.Root.Fields[0].Name
	nullScalar := scalarNullPlant(u, baseJSON)
	nullScalarMode := "must_refuse"
	if u.Root.Fields[0].Kind == gOptScalar && nullScalar == withFieldReplaced(baseJSON, f0, "null") {
		nullScalarMode = "must_accept"
	}
	var specs []plantSpec
	// (1) null for a scalar and for a table
	specs = append(specs, plantSpec{name: "null_scalar", mode: nullScalarMode, text: nullScalar})
	specs = append(specs, tableNullPlants(u, baseJSON)...)
	// (2) -0
	specs = append(specs, plantSpec{name: "minus_zero_" + f0, mode: "either", text: withFieldReplaced(baseJSON, f0, "-0")})
	for _, f := range u.Root.Fields {
		if f.Kind == gFloat32 || f.Kind == gFloat64 {
			specs = append(specs, plantSpec{name: "minus_zero_float_" + f.Name, mode: "must_accept", text: withFieldReplaced(baseJSON, f.Name, "-0")})
			break
		}
	}
	// (3) 1e999
	specs = append(specs, plantSpec{name: "overflow_1e999_" + f0, mode: "must_refuse", text: withFieldReplaced(baseJSON, f0, "1e999")})
	for _, f := range u.Root.Fields {
		if f.Kind == gFloat32 || f.Kind == gFloat64 || f.Kind == gInt || f.Kind == gUint {
			if f.Name != f0 {
				specs = append(specs, plantSpec{name: "overflow_1e999_" + f.Name, mode: "must_refuse", text: withFieldReplaced(baseJSON, f.Name, "1e999")})
			}
			break
		}
	}
	// (4) NaN
	nanMode := "must_refuse"
	if sabotage {
		// Negative control: tell the probe to expect a refused plant (NaN) to be accepted.
		nanMode = "must_accept"
	}
	specs = append(specs, plantSpec{name: "nan_" + f0, mode: nanMode, text: withFieldReplaced(baseJSON, f0, "NaN")})
	// (5) a lone surrogate escape
	specs = append(specs, plantSpec{name: "lone_surrogate_" + f0, mode: "either", text: withFieldReplaced(baseJSON, f0, `"\ud800"`)})
	for _, f := range u.Root.Fields {
		if f.Kind == gString && f.Name != f0 {
			mode := "must_accept"
			if f.StrN < 3 {
				mode = "must_refuse"
			}
			specs = append(specs, plantSpec{name: "lone_surrogate_str_" + f.Name, mode: mode, text: withFieldReplaced(baseJSON, f.Name, `"\ud800"`)})
			break
		}
	}
	// (6) a leading BOM is skipped on read and never written (SPEC-TABLES 16.2
	// and 16.12; JSONTestSuite i_structure_UTF-8_BOM_empty_object.json: accept).
	specs = append(specs, plantSpec{name: "leading_bom", mode: "must_accept", text: "\ufeff" + baseJSON})
	// (7) trailing data after the object
	specs = append(specs, plantSpec{name: "trailing_data", mode: "must_refuse", text: baseJSON + " {}"})
	// (8) a duplicate key
	specs = append(specs, plantSpec{name: "duplicate_key_" + f0, mode: "must_accept", text: withDuplicateKey(baseJSON, f0)})
	// (9) nesting of 10^6 arrays (token "__DEEP_1E6__" is expanded in the probe before timing/memstats)
	specs = append(specs, plantSpec{name: "nested_1e6_arrays", mode: "must_refuse", text: withFieldReplaced(baseJSON, f0, `"__DEEP_1E6__"`)})
	return specs
}

var tableJsonPlantsProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func record(div *[]string, format string, args ...any) {
	*div = append(*div, fmt.Sprintf(format, args...))
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestJsonPlantsProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	raw, err := os.ReadFile(dir + "/plants.txt")
	if err != nil {
		t.Fatal(err)
	}
	deep := strings.Repeat("[", 1000000) + strings.Repeat("]", 1000000)
	var div []string

	// First verify the base JSON itself is accepted cleanly.
	baseBytes, err := os.ReadFile(dir + "/base.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseVal Root
	var baseRep TableReport
	RootReset(&baseVal)
	if !RootFromJson(&baseVal, baseBytes, &baseRep) || baseRep != (TableReport{}) {
		record(&div, "baserefuse %+v %s", baseRep, b64(baseBytes))
	}

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed plant line: %q", line)
		}
		name, mode := parts[0], parts[1]
		decoded, err := base64.StdEncoding.DecodeString(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(decoded, []byte(` + "`" + `"__DEEP_1E6__"` + "`" + `)) {
			decoded = bytes.Replace(decoded, []byte(` + "`" + `"__DEEP_1E6__"` + "`" + `), []byte(deep), 1)
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					record(&div, "plantpanic %s %v", name, r)
				}
			}()
			var pv Root
			var pr TableReport
			RootReset(&pv)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			ok := RootFromJson(&pv, decoded, &pr)
			runtime.ReadMemStats(&after)
			if after.TotalAlloc-before.TotalAlloc > 4<<20 {
				record(&div, "plantalloc %s %d", name, after.TotalAlloc-before.TotalAlloc)
			}

			refused := !ok || pr.Err() != nil
			if refused {
				if pr.Err() == nil || pr.Err().Error() == "" {
					record(&div, "plantunnamedrefusal %s ok=%v report=%+v", name, ok, pr)
				}
				if mode == "must_accept" {
					record(&div, "plantrefused %s ok=%v err=%v", name, ok, pr.Err())
				}
			} else {
				if mode == "must_refuse" {
					record(&div, "plantaccepted %s report=%+v", name, pr)
				}
			}

			// Whenever RootFromJson returns true (accepted or tolerantly clamped/skipped),
			// the resulting value must measure, save, and write JSON that reads back equal.
			if ok {
				m, mreason := RootMeasureReason(&pv)
				if m < 0 || mreason != nil || RootMeasure(&pv) != m {
					record(&div, "plantmeasure %s m=%d reason=%v", name, m, mreason)
					return
				}
				wire := make([]byte, m)
				if RootSave(&pv, wire) != m {
					record(&div, "plantsave %s", name)
					return
				}
				jm := RootToJsonMeasure(&pv)
				if jm <= 0 {
					record(&div, "planttojsonmeasure %s jm=%d", name, jm)
					return
				}
				tj := make([]byte, jm)
				if RootToJson(&pv, tj) != jm {
					record(&div, "planttojson %s", name)
					return
				}
				var v2 Root
				var r2 TableReport
				RootReset(&v2)
				if !RootFromJson(&v2, tj, &r2) || r2.Err() != nil {
					record(&div, "plantroundtriprefuse %s err=%v %s", name, r2.Err(), b64(tj))
					return
				}
				tj2 := make([]byte, RootToJsonMeasure(&v2))
				if RootToJson(&v2, tj2) != int64(len(tj2)) || !bytes.Equal(tj2, tj) {
					record(&div, "plantroundtripjson %s %s %s", name, b64(tj), b64(tj2))
				}
				w2 := make([]byte, RootMeasure(&v2))
				if RootSave(&v2, w2) != int64(len(w2)) || !bytes.Equal(w2, wire) {
					record(&div, "plantroundtripwire %s", name)
				}
			}
		}()
	}

	if len(div) > 0 {
		_ = os.WriteFile(dir+"/divergences.txt", []byte(strings.Join(div, "\n")+"\n"), 0o600)
		t.Fatalf("%d divergence(s)", len(div))
	}
}
`

func runJsonPlantsSeed(t *testing.T, r *rand.Rand, seed int, sabotage bool) (unexpected []string, expected []string) {
	t.Helper()
	src, u := randomSchema(r)
	unit := tableUnit(t, src)
	dir := t.TempDir()
	baseJSON := validUnitJSON(r, u)
	if err := os.WriteFile(filepath.Join(dir, "base.json"), []byte(baseJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	specs := buildPlantSpecs(u, baseJSON, sabotage)
	var manifest strings.Builder
	for _, s := range specs {
		fmt.Fprintf(&manifest, "%s\t%s\t%s\n", s.name, s.mode, b64.EncodeToString([]byte(s.text)))
	}
	if err := os.WriteFile(filepath.Join(dir, "plants.txt"), []byte(manifest.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir}
	out, err := runGeneratedTableUnit(t, unit, tableJsonPlantsProbe, env, "-race")
	divBytes, _ := os.ReadFile(filepath.Join(dir, "divergences.txt"))
	for line := range strings.SplitSeq(strings.TrimSpace(string(divBytes)), "\n") {
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
	}
	return unexpected, expected
}

// TestRandomTableJsonPlants generates random valid table units for seeds
// SCHEMA_TABLE_DIFF_FROM..TO (default 0..4), plants the nine hostile/boundary
// JSON cases into a valid randomJSON text of each unit, and runs RootFromJson
// under recover and -race, plus one sabotaged negative-control run.
func TestRandomTableJsonPlants(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units)")
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
			unexpected, expected := runJsonPlantsSeed(t, rand.New(rand.NewSource(int64(seed))), seed, false)
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			for _, e := range unexpected {
				t.Errorf("%s", e)
			}
		})
	}
	t.Run("negative_control", func(t *testing.T) {
		t.Parallel()
		unexpected, _ := runJsonPlantsSeed(t, rand.New(rand.NewSource(0)), 0, true)
		if len(unexpected) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: a plant whose refusal was expected as acceptance left the gate green")
		}
		if !strings.Contains(strings.Join(unexpected, "\n"), "unexpected divergence") {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on an unexpected divergence: %v", unexpected)
		}
	})
}
