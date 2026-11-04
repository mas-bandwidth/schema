package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type implementationUnits map[string]*ir.Unit

func (u implementationUnits) get(key string) (*ir.Unit, error) {
	if unit := u[key]; unit != nil {
		return unit, nil
	}
	return nil, errors.New("missing unit")
}

type implementationGeneratorFunc func(*ir.Unit, string, compiler.Options) (map[string][]byte, error)

func (f implementationGeneratorFunc) Generate(u *ir.Unit, lang string, opts compiler.Options) (map[string][]byte, error) {
	return f(u, lang, opts)
}

func implementationUnit(t *testing.T, source string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte("package probe\n"+source), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func implementationFixture(t *testing.T) *ir.Unit {
	return implementationUnit(t, "table Used\n{\n value int32\n}\ntable Feature\n{\n at timestamp\n}\ntable Parent\n{\n child *Feature\n next *Parent\n}\n")
}

func implementationInput(t *testing.T, u *ir.Unit, root, text string) Instance {
	t.Helper()
	model := tabletext.NewModel(u)
	value := model.New(u.Tables[root])
	var report tabletext.Report
	if !model.Read(value, []byte(text), &report) || !report.Silent() {
		t.Fatalf("fixture JSON failed: %+v", report)
	}
	return implementationValueInput(t, model, value)
}

func implementationValueInput(t *testing.T, model *tabletext.Model, value *tabletext.Instance) Instance {
	t.Helper()
	wire, err := encodeImplementationWitness(model, value)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inst := Instance{Name: "witness", Unit: "unit", Root: value.Def.Name, Wire: filepath.Join(dir, "wire"), JSON: filepath.Join(dir, "text")}
	for name, data := range map[string][]byte{inst.Wire: wire, inst.JSON: text} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return inst
}

func implementationGenerated(*ir.Unit, string, compiler.Options) (map[string][]byte, error) {
	return map[string][]byte{"unit.go": []byte("package unit")}, nil
}

func implementationProbe(t *testing.T, u *ir.Unit, inst Instance, feature string) ([]string, int) {
	t.Helper()
	return implementationAudit(&Manifest{Instances: []Instance{inst}}, []string{"go"}, []string{feature},
		func(string) ([]string, bool) { return []string{feature}, true },
		implementationUnits{"unit": u}, implementationGeneratorFunc(implementationGenerated))
}

func TestImplementationCannotOmitUnclaimedFeatures(t *testing.T) {
	u := implementationFixture(t)
	full := func(string) ([]string, bool) { return []string{"timestamp"}, true }
	generated := implementationGeneratorFunc(implementationGenerated)
	for _, tc := range []struct {
		name, root, text, refusal, want string
		registration                    func(string) ([]string, bool)
		gen                             implementationGenerator
	}{
		{"complete witness", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "", "", full, generated},
		{"active pointer with recursive declaration", "Parent", "{\"child\":{\"at\":\"1970-01-01T00:00:01Z\"}}", "", "", full, generated},
		{"nil pointer cannot activate declaration", "Parent", "{}", "", "no oracle-qualified active instance", full, generated},
		{"default timestamp cannot qualify", "Feature", "{}", "", "no oracle-qualified active instance", full, generated},
		{"unused feature in same unit", "Used", "{\"value\":1}", "", "no oracle-qualified active instance", full, generated},
		{"required feature not claimed", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "", "not implemented", func(string) ([]string, bool) { return nil, true }, generated},
		{"target missing", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "", "registration is missing", func(string) ([]string, bool) { return nil, false }, generated},
		{"refusal is not positive", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "unsupported", "no oracle-qualified active instance", full, generated},
		{"registration cannot mask emitter refusal", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "", "cannot generate", full, implementationGeneratorFunc(func(*ir.Unit, string, compiler.Options) (map[string][]byte, error) {
			return nil, errors.New("unsupported")
		})},
		{"empty generator", "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}", "", "generated no files", full, implementationGeneratorFunc(func(*ir.Unit, string, compiler.Options) (map[string][]byte, error) { return nil, nil })},
		{"missing root", "Missing", "{}", "", "no table root", full, generated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.root
			if u.Tables[root] == nil {
				root = "Used"
			}
			inst := implementationInput(t, u, root, tc.text)
			inst.Root, inst.GoWireRefusal = tc.root, tc.refusal
			m := &Manifest{Instances: []Instance{inst}}
			p, witnesses := implementationAudit(m, []string{"go"}, []string{"timestamp"}, tc.registration, implementationUnits{"unit": u}, tc.gen)
			text := strings.Join(p, "\n")
			if tc.want == "" && (len(p) != 0 || witnesses != 1) {
				t.Fatalf("positive witnesses=%d: %s", witnesses, text)
			}
			if tc.want != "" && !strings.Contains(text, tc.want) {
				t.Fatalf("want %q in %s", tc.want, text)
			}
		})
	}
	if len(u.Tables) != 3 || len(u.Files) != 1 || len(u.Files[0].Tables) != 3 {
		t.Fatal("root witness check mutated compiler IR")
	}
	if p := implementationProblems(&Manifest{}, []string{"go"}, nil, full, implementationUnits{}, generated); len(p) == 0 {
		t.Fatal("empty feature universe passed")
	}
	if p := implementationProblems(&Manifest{}, []string{"go"}, []string{"timestamp"}, full, implementationUnits{}, generated); len(p) == 0 {
		t.Fatal("empty corpus passed")
	}
}

func TestImplementationRequiresActualFeatureValues(t *testing.T) {
	// These are real input files built by the existing neutral engines, not
	// stand-in manifest flags. Each dormant form must fail despite the same
	// reachable declaration and the same claimed generator registration.
	for _, tc := range []struct{ feature, schema, active, dormant string }{
		{"required", "table Root\n{\n name string(32) | required\n}\n", "{\"name\":\"live\"}", "{\"name\":\"\"}"},
		{"one-sided-range", "table Root\n{\n n int64 | min = 0\n}\n", "{\"n\":1}", "{}"},
		{"text-list", "table Root\n{\n names [..4]string(8)\n}\n", "{\"names\":[\"live\"]}", "{\"names\":[]}"},
		{"text-list-total", "table Root\n{\n names [..4]string(8) | total = 12\n}\n", "{\"names\":[\"live\"]}", "{\"names\":[\"\"]}"},
		{"array-root", "table Root | json = \"array\"\n{\n names [..4]string(8)\n}\n", "[\"live\"]", "[]"},
		{"optional-map", "table Root\n{\n names ?map[string(8)]int32\n}\n", "{\"names\":{\"live\":1}}", "{\"names\":{}}"},
		{"optional-variable-closure", "table Leaf\n{\n n int32\n}\ntable Inner\n{\n child *Leaf\n}\ntable Root\n{\n inner ?Inner\n}\n", "{\"inner\":{\"child\":{\"n\":1}}}", "{\"inner\":{}}"},
	} {
		t.Run(tc.feature+"/"+tc.schema, func(t *testing.T) {
			u := implementationUnit(t, tc.schema)
			if !slices.Contains(compiler.UnitConstructs(u), tc.feature) {
				t.Fatalf("fixture does not declare %s", tc.feature)
			}
			for _, row := range []struct {
				text string
				pass bool
			}{{tc.active, true}, {tc.dormant, false}} {
				inst := implementationInput(t, u, "Root", row.text)
				problems, count := implementationProbe(t, u, inst, tc.feature)
				if row.pass && (len(problems) != 0 || count != 1) {
					t.Fatalf("active input failed: %v (count %d)", problems, count)
				}
				if !row.pass && (len(problems) == 0 || count != 0) {
					t.Fatalf("dormant input passed: %v (count %d)", problems, count)
				}
			}
		})
	}
}

func TestImplementationRefusesAnOverBoundOracleResult(t *testing.T) {
	u := implementationUnit(t, "table Root\n{\n name string(2) | required\n}\n")
	inst := implementationInput(t, u, "Root", "{\"name\":\"ok\"}")
	if err := os.WriteFile(inst.JSON, []byte("{\"name\":\"overlong\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	problems, count := implementationProbe(t, u, inst, "required")
	got := strings.Join(problems, "\n")
	if count != 0 || !strings.Contains(got, "positive JSON does not decode cleanly") || !strings.Contains(got, "Refused:true") {
		t.Fatalf("over-bound oracle response qualified: %s / %d", got, count)
	}
}

func TestImplementationOnlyVisitsSelectedValues(t *testing.T) {
	u := implementationUnit(t, "table Feature\n{\n at timestamp\n}\nunion Choice\n{\n feature Feature\n other int32\n}\ntable Root\n{\n choice Choice\n}\n")
	for _, row := range []struct {
		text string
		pass bool
	}{
		{"{\"choice\":{\"feature\":{\"at\":\"1970-01-01T00:00:01Z\"}}}", true},
		{"{\"choice\":{\"other\":1}}", false},
		{"{}", false},
	} {
		p, n := implementationProbe(t, u, implementationInput(t, u, "Root", row.text), "timestamp")
		if row.pass && (len(p) != 0 || n != 1) {
			t.Fatalf("selected feature failed: %v", p)
		}
		if !row.pass && (len(p) == 0 || n != 0) {
			t.Fatalf("unselected feature qualified: %v", p)
		}
	}
	u = implementationUnit(t, "table Root\n{\n active bool\n if active\n {\n  at timestamp\n }\n}\n")
	for _, active := range []bool{false, true} {
		model := tabletext.NewModel(u)
		value := model.New(u.Tables["Root"])
		value.Fields[0].Cell.B = active
		value.Fields[1].Cell.I, value.Fields[1].Cell.U = 1, 1
		p, n := implementationProbe(t, u, implementationValueInput(t, model, value), "timestamp")
		if active && (len(p) != 0 || n != 1) {
			t.Fatalf("active guard failed: %v", p)
		}
		if !active && (len(p) == 0 || n != 0) {
			t.Fatalf("guarded-out feature qualified: %v", p)
		}
	}
}

func TestImplementationIgnoresUnusedArrayCapacity(t *testing.T) {
	u := implementationUnit(t, "table Feature\n{\n at timestamp\n}\ntable Root\n{\n rows [..2]Feature\n}\n")
	model := tabletext.NewModel(u)
	root := model.New(u.Tables["Root"])
	f := &root.Fields[0]
	// The capacity contains a feature, but Count=0 means no such value is on
	// the wire. Merely traversing all allocated Elems would create coverage.
	f.Elems[0].Tab.Fields[0].Cell.I = 1
	f.Elems[0].Tab.Fields[0].Cell.U = 1
	for _, count := range []int{0, 1} {
		f.Count = count
		problems, witnesses := implementationProbe(t, u, implementationValueInput(t, model, root), "timestamp")
		if count == 0 && (len(problems) == 0 || witnesses != 0) {
			t.Fatalf("unused capacity qualified: %v", problems)
		}
		if count == 1 && (len(problems) != 0 || witnesses != 1) {
			t.Fatalf("live array value failed: %v", problems)
		}
	}
}

func TestImplementationInputMustBeOracleQualified(t *testing.T) {
	u := implementationFixture(t)
	for _, kind := range []string{"missing-wire", "damaged-wire", "extra-wire", "missing-json", "mismatched-json", "damaged-json", "no-text"} {
		t.Run(kind, func(t *testing.T) {
			inst := implementationInput(t, u, "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}")
			var err error
			switch kind {
			case "missing-wire":
				err = os.Remove(inst.Wire)
			case "damaged-wire":
				err = os.WriteFile(inst.Wire, []byte{255}, 0600)
			case "extra-wire":
				wire, readErr := os.ReadFile(inst.Wire)
				if readErr != nil {
					t.Fatal(readErr)
				}
				err = os.WriteFile(inst.Wire, append(wire, 0), 0600)
			case "missing-json":
				err = os.Remove(inst.JSON)
			case "mismatched-json":
				err = os.WriteFile(inst.JSON, []byte("{}"), 0600)
			case "damaged-json":
				err = os.WriteFile(inst.JSON, []byte("{"), 0600)
			case "no-text":
				inst.NoText = true
			}
			if err != nil {
				t.Fatal(err)
			}
			problems, _ := implementationProbe(t, u, inst, "timestamp")
			if len(problems) == 0 {
				t.Fatalf("%s qualified", kind)
			}
		})
	}
}

func TestImplementationRootProperties(t *testing.T) {
	u := implementationUnit(t, "table Root | forms = \"wire,json\"\n{\n n int32\n}\n")
	if p, n := implementationProbe(t, u, implementationInput(t, u, "Root", "{\"n\":1}"), "forms"); len(p) != 0 || n != 1 {
		t.Fatalf("actual permitted root entry failed: %v", p)
	}
	u = implementationUnit(t, "table Child | integrity = \"crc32c\"\n{\n n int32\n}\ntable Root\n{\n child Child\n}\n")
	inst := implementationInput(t, u, "Root", "{\"child\":{\"n\":1}}")
	if p, n := implementationProbe(t, u, inst, "integrity"); len(p) == 0 || n != 0 {
		t.Fatalf("nested annotation credited root integrity: %v / %d", p, n)
	}
}

func TestImplementationIntegrityCRC32C(t *testing.T) {
	u := implementationUnit(t, "table Root | integrity = \"crc32c\"\n{\n n int32\n}\n")
	inst := implementationInput(t, u, "Root", "{\"n\":1}")
	sealed, err := os.ReadFile(inst.Wire)
	if err != nil {
		t.Fatal(err)
	}
	if p, n := implementationProbe(t, u, inst, "integrity"); len(p) != 0 || n != 1 {
		t.Fatalf("actual sealed positive input failed: %v / %d", p, n)
	}
	// Each changed byte is real input damage. A checksum check that accepts
	// every trailer (or one which never runs) fails these controls.
	for i := range sealed {
		damaged := append([]byte(nil), sealed...)
		damaged[i] ^= 1
		if err := os.WriteFile(inst.Wire, damaged, 0600); err != nil {
			t.Fatal(err)
		}
		p, n := implementationProbe(t, u, inst, "integrity")
		if len(p) == 0 || n != 0 {
			t.Fatalf("byte %d damage qualified: %v", i, p)
		}
		if i > 0 && !strings.Contains(strings.Join(p, "\n"), "integrity_mismatch") {
			t.Fatalf("byte %d decoded before checksum: %v", i, p)
		}
	}
	for _, short := range [][]byte{nil, {1}, {1, 0, 0, 0}, sealed[:len(sealed)-4]} {
		if err := os.WriteFile(inst.Wire, short, 0600); err != nil {
			t.Fatal(err)
		}
		if p, n := implementationProbe(t, u, inst, "integrity"); len(p) == 0 || n != 0 {
			t.Fatalf("unsealed/short input qualified: %v", p)
		}
	}
	// An unsupported form and a malformed form-1 body under a bad CRC are
	// each refused, and neither qualifies; which condition a read names first
	// is its own diagnostic (docs/SPEC-TABLES.md §6.5).
	for _, wire := range [][]byte{{2}, {1, 255, 0, 0, 0, 0}} {
		if err := os.WriteFile(inst.Wire, wire, 0600); err != nil {
			t.Fatal(err)
		}
		p, n := implementationProbe(t, u, inst, "integrity")
		got := strings.Join(p, "\n")
		if n != 0 || !strings.Contains(got, "Refused:true") {
			t.Fatalf("%x qualified or was not refused: %s", wire, got)
		}
	}
	if err := os.WriteFile(inst.Wire, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inst.JSON, []byte("{\"n\":2}"), 0600); err != nil {
		t.Fatal(err)
	}
	if p, n := implementationProbe(t, u, inst, "integrity"); n != 0 || !strings.Contains(strings.Join(p, "\n"), "positive JSON does not reproduce") {
		t.Fatalf("different JSON value qualified sealed wire: %v", p)
	}
	u.Tables["Root"].Integrity = "future-unknown"
	if p, n := implementationProbe(t, u, inst, "integrity"); n != 0 || !strings.Contains(strings.Join(p, "\n"), "no qualified witness oracle") {
		t.Fatalf("unknown integrity kind qualified: %v", p)
	}
}

// This golden is independently pinned by the generated-Go native integrity
// test (internal/codegen/gotable/integrity_test.go), not constructed by the
// neutral codec under test. The 53-byte plain file plus little-endian 0x1d23b551
// locks the polynomial, covered extent, initial/final XOR and byte order.
func TestImplementationIntegrityMatchesGeneratedGoGolden(t *testing.T) {
	u := implementationUnit(t, "table Ledger | integrity = \"crc32c\"\n{\n n int64 | min = 0, max = 9007199254740993\n label string(16)\n count uint16\n}\n")
	const plainHex = "010105e803000000000000020c026f6b030707000071f801864ce363af3d62cb8fecfcf73974a279448ee2e5b10300000000000000"
	want, err := hex.DecodeString(plainHex + "51b5231d")
	if err != nil {
		t.Fatal(err)
	}
	inst := implementationInput(t, u, "Ledger", "{\"n\":1000,\"label\":\"ok\",\"count\":7}")
	actual, err := os.ReadFile(inst.Wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, want) {
		t.Fatalf("neutral codec changed independent generated-Go golden: %x", actual)
	}
	if p, n := implementationProbe(t, u, inst, "integrity"); len(p) != 0 || n != 1 {
		t.Fatalf("golden refused: %v", p)
	}
	model, value, _, err := decodeImplementationWitness(u, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("canonical sealed file changed: %x", body)
	}
}

func TestImplementationDecodesEachInputOnceAcrossLanguages(t *testing.T) {
	u := implementationFixture(t)
	inst := implementationInput(t, u, "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}")
	seen := map[string]int{}
	gen := implementationGeneratorFunc(func(_ *ir.Unit, lang string, _ compiler.Options) (map[string][]byte, error) {
		seen[lang]++
		// Decoding is completed before generation. Removing the files here
		// would break a second oracle read for the next target.
		if lang == "a" {
			if err := os.Remove(inst.Wire); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(inst.JSON); err != nil {
				t.Fatal(err)
			}
		}
		return implementationGenerated(nil, lang, nil)
	})
	p, count := implementationAudit(&Manifest{Instances: []Instance{inst}}, []string{"a", "b"}, []string{"timestamp"},
		func(string) ([]string, bool) { return []string{"timestamp"}, true }, implementationUnits{"unit": u}, gen)
	if len(p) != 0 || count != 1 || seen["a"] != 1 || seen["b"] != 1 {
		t.Fatalf("per-input oracle contract failed: %v / %d / %v", p, count, seen)
	}
}

func TestOriginalImplementationUsesAllRequiredConstructs(t *testing.T) {
	t.Skip("layer 3: the implementation corpus's form-1 wires under fixed-declared roots read as previous_form (red at base f7bd63363), re-pinned per docs/plans/strict-only.md (b)")
	t.Chdir("../../..")
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	problems := strings.Join(originalImplementationProblems(m), "\n")
	for _, lang := range originalNine {
		have, _ := compiler.Carried(lang)
		for _, feature := range compiler.CarryConstructs() {
			if !slices.Contains(have, feature) && !strings.Contains(problems, lang+": required Schema construct "+feature+" is not implemented") {
				t.Fatalf("unclaimed %s/%s disappeared from acceptance", lang, feature)
			}
		}
	}
	var out strings.Builder
	if err := implementationReport(&out, m, "go"); err != nil || !strings.Contains(out.String(), "IMPLEMENTATION_PREREQUISITES_PASS") {
		t.Fatalf("populated real corpus did not qualify: err=%v, output=%s", err, out.String())
	}
}

func TestImplementationFormAndIntegrityNeedPopulatedPayload(t *testing.T) {
	u := implementationUnit(t, `table Root | forms="wire,json", integrity="crc32c" { value int32 }`)
	for _, input := range []string{"{}", `{"value":7}`} {
		inst := implementationInput(t, u, "Root", input)
		model, value, text, err := decodeImplementationWitness(u, inst)
		if err != nil {
			t.Fatal(err)
		}
		got := activeImplementationConstructs(model, value, text)
		for _, feature := range []string{"forms", "integrity"} {
			if got[feature] != (input != "{}") {
				t.Fatalf("input=%s feature=%s active=%v", input, feature, got)
			}
		}
	}
}

func TestPopulatedFeatureWitnessesCoverEveryRequiredConstruct(t *testing.T) {
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	// the two WITNESS instances by name: the unit also carries the canonical
	// writer's rows (docs/SPEC-TABLES.md §16.14), which reuse its roots to pin
	// bytes and are not what this audit counts
	m := &Manifest{Units: manifest.Units}
	for _, inst := range manifest.Instances {
		if inst.Name == "feature_full" || inst.Name == "feature_array_root" {
			m.Instances = append(m.Instances, inst)
		}
	}
	if len(m.Instances) != 2 {
		t.Fatalf("expected two active feature witnesses, found %d", len(m.Instances))
	}
	required := compiler.CarryConstructs()
	problems, count := implementationAudit(m, []string{"go", "js", "c"}, required, compiler.Carried, newUnits(m), compiler.New())
	if len(problems) != 0 || count != 2 {
		t.Fatalf("populated witness audit: count=%d problems=%v", count, problems)
	}
	// Deleting either populated root must lose real runtime coverage.
	for i := range m.Instances {
		less := *m
		less.Instances = append(append([]Instance{}, m.Instances[:i]...), m.Instances[i+1:]...)
		problems, _ := implementationAudit(&less, []string{"go"}, required, compiler.Carried, newUnits(&less), compiler.New())
		if len(problems) == 0 {
			t.Fatalf("deleting witness %s still passed", m.Instances[i].Name)
		}
	}
}

// Ordinary schema units are implementation obligations too. The special
// construct registry must not let an otherwise unsupported positive unit hide.
func TestImplementationGeneratesOrdinaryPositiveUnits(t *testing.T) {
	feature := implementationUnit(t, "table Feature\n{\n at timestamp\n}\n")
	plain := implementationUnit(t, "table Plain\n{\n values [2]int32\n}\n")
	a := implementationInput(t, feature, "Feature", "{\"at\":\"1970-01-01T00:00:01Z\"}")
	a.Unit = "feature"
	b := implementationInput(t, plain, "Plain", "{\"values\":[1,2]}")
	b.Unit = "plain"
	for _, mode := range []string{"ok", "refused", "empty", "damaged"} {
		t.Run(mode, func(t *testing.T) {
			input := b
			if mode == "damaged" {
				input.Wire = filepath.Join(t.TempDir(), "damaged.bin")
				if err := os.WriteFile(input.Wire, []byte{255}, 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := map[string]int{}
			gen := implementationGeneratorFunc(func(u *ir.Unit, lang string, opts compiler.Options) (map[string][]byte, error) {
				key := lang + "/feature"
				if u == plain {
					key = lang + "/plain"
				}
				calls[key]++
				if u == plain && mode == "refused" {
					return nil, errors.New("ordinary shape unsupported")
				}
				if u == plain && mode == "empty" {
					return nil, nil
				}
				return implementationGenerated(u, lang, opts)
			})
			problems, n := implementationAudit(&Manifest{Instances: []Instance{a, input, input}}, []string{"go", "rust"}, []string{"timestamp"},
				func(string) ([]string, bool) { return []string{"timestamp"}, true }, implementationUnits{"feature": feature, "plain": plain}, gen)
			if mode == "ok" {
				if len(problems) != 0 || n != 1 {
					t.Fatalf("ordinary positive input: witnesses=%d problems=%v", n, problems)
				}
				for _, key := range []string{"go/feature", "go/plain", "rust/feature", "rust/plain"} {
					if calls[key] != 1 {
						t.Errorf("generate %s called %d times, want once per unit and language", key, calls[key])
					}
				}
			} else {
				want := map[string]string{"refused": "ordinary shape unsupported", "empty": "unit plain generated no files", "damaged": "positive wire does not decode cleanly"}[mode]
				if !strings.Contains(strings.Join(problems, "\n"), want) {
					t.Fatalf("%s input escaped full implementation prerequisite: %v", mode, problems)
				}
			}
		})
	}
}
