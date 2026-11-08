package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestHarnessIsReadThenWrite(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	manifestPath := root + "testdata/conformance/tables/MANIFEST.txt"
	jsonDir := root + "testdata/conformance/tables/json"
	reportsPath := root + "testdata/conformance/tables/reports.txt"

	// 1. Check harness row types and surfaces have no counter, size, reason-name or position columns.
	retiredFields := map[string]bool{
		"Counts":        true,
		"Unknown":       true,
		"KindMismatch":  true,
		"Widened":       true,
		"Clamped":       true,
		"Duplicate":     true,
		"Malformed":     true,
		"Damaged":       true,
		"Load":          true,
		"Retained":      true,
		"RetainLost":    true,
		"SaveLost":      true,
		"Measure":       true,
		"RegionSize":    true,
		"Reason":        true,
		"Refusals":      true,
		"GoWireRefusal": true,
		"WireRefusedBy": true,
		"CarriedBy":     true,
		"Uncarried":     true,
		"Position":      true,
	}
	mt := reflect.TypeFor[Manifest]()
	for sf := range mt.Fields() {
		if retiredFields[sf.Name] {
			t.Errorf("Manifest still carries retired field %s", sf.Name)
		}
		if sf.Type.Kind() == reflect.Slice && sf.Type.Elem().Kind() == reflect.Struct {
			et := sf.Type.Elem()
			for ef := range et.Fields() {
				if retiredFields[ef.Name] {
					t.Errorf("Manifest row type %s still carries retired field %s", et.Name(), ef.Name)
				}
			}
		}
	}
	for _, s := range surfaces {
		if s == "cook-reason" || s == "block-reason" || s == "strict-reason" {
			t.Errorf("surfaces still carries retired reason surface %q", s)
		}
	}

	// 2. Check MANIFEST.txt and reports.txt have no counter, size, reason-name or position columns.
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for lineNo, line := range strings.Split(string(rawManifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "refusal":
			t.Errorf("MANIFEST.txt:%d: retired refusal reason line %q", lineNo+1, line)
		case "instance":
			for _, marker := range f[5:] {
				if strings.HasPrefix(marker, "go-refusal=") || strings.HasPrefix(marker, "refused-by=") {
					t.Errorf("MANIFEST.txt:%d: instance %s carries retired marker %q", lineNo+1, f[1], marker)
				}
			}
		case "framing":
			if len(f) != 7 || (f[5] != "read" && f[5] != "refused") {
				t.Errorf("MANIFEST.txt:%d: framing %s has non-verdict column %q", lineNo+1, f[1], f[5])
			}
		case "json-hostile":
			if len(f) != 6 || (f[5] != "read" && f[5] != "refused") {
				t.Errorf("MANIFEST.txt:%d: json-hostile %s has retired columns %v", lineNo+1, f[1], f[5:])
			}
		case "retain":
			if len(f) != 8 {
				t.Errorf("MANIFEST.txt:%d: retain %s has %d columns (want 8 without counters)", lineNo+1, f[1], len(f))
			}
		}
	}

	rawReports, err := os.ReadFile(reportsPath)
	if err != nil {
		t.Fatal(err)
	}
	reportVerdicts := map[string]string{}
	for lineNo, line := range strings.Split(string(rawReports), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || (f[1] != "read" && f[1] != "refused") {
			t.Errorf("reports.txt:%d: report %q is not <case> read|refused", lineNo+1, line)
			continue
		}
		reportVerdicts[f[0]] = f[1]
	}

	// 3. Check every readable row has canonical wire, JSON and text bytes, and every unreadable row is in the refuse set.
	m, err := ReadManifest(manifestPath, jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j := range m.Units[i].Paths {
			m.Units[i].Paths[j] = filepath.Join(root, m.Units[i].Paths[j])
		}
	}
	u := newUnits(m)

	checkValueBytes := func(caseName, unitKey, rootName, valuePath string) []byte {
		rawText, err := os.ReadFile(filepath.Join(root, valuePath))
		if err != nil {
			t.Errorf("%s: missing canonical value file %s: %v", caseName, valuePath, err)
			return nil
		}
		unit, err := u.get(unitKey)
		if err != nil {
			t.Errorf("%s: unit %s: %v", caseName, unitKey, err)
			return nil
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(rootName)
		if def == nil {
			t.Errorf("%s: missing root %s.%s", caseName, unitKey, rootName)
			return nil
		}
		inst := model.New(def)
		var rep tabletext.Report
		if !strictReadJSON(model, inst, rawText, &rep) {
			t.Errorf("%s: canonical value %s did not read cleanly", caseName, valuePath)
			return nil
		}
		textBack, err := model.Write(inst)
		if err != nil || !bytes.Equal(oneText(textBack), rawText) {
			t.Errorf("%s: value file %s is not canonical JSON/text", caseName, valuePath)
		}
		wireBack, err := tablewire.Encode(model, inst)
		if err != nil || len(wireBack) == 0 {
			t.Errorf("%s: canonical value %s failed wire encode: %v", caseName, valuePath, err)
			return nil
		}
		return wireBack
	}

	// checkSaveWire holds the committed canonical write-back wire to the engine
	// that generated it, and to the harness's expectation. The value's JSON
	// text and its wire are the one value in its two forms; a leg that writes
	// the text back but not the wire has not returned the whole value.
	checkSaveWire := func(caseName, savePath string, engine []byte) {
		if engine == nil {
			return
		}
		golden, err := os.ReadFile(savePath)
		if err != nil {
			t.Errorf("%s: missing canonical write-back wire %s: %v", caseName, savePath, err)
			return
		}
		if !bytes.Equal(golden, engine) {
			t.Errorf("%s: write-back wire %s is not the engine's canonical bytes — run: make conformance-generate", caseName, savePath)
		}
	}

	settle := func(wants []expectation, err error) []expectation {
		if err != nil {
			t.Fatal(err)
		}
		return wants
	}
	// The harness resolves framing and hostile VALUES against the working
	// directory, which is the repository root at run time; this test runs in
	// its own directory, so root those paths in a copy for expectations only.
	mForExpect := *m
	mForExpect.Framings = append([]FramingCase(nil), m.Framings...)
	for i := range mForExpect.Framings {
		if v := mForExpect.Framings[i].Value; v != "" {
			mForExpect.Framings[i].Value = filepath.Join(root, v)
		}
	}
	mForExpect.Hostiles = append([]Hostile(nil), m.Hostiles...)
	for i := range mForExpect.Hostiles {
		if v := mForExpect.Hostiles[i].Value; v != "" {
			mForExpect.Hostiles[i].Value = filepath.Join(root, v)
		}
	}
	reportWants := map[string][]byte{}
	for _, e := range settle(expectations(&mForExpect, "report", reportVerdicts, jsonDir)) {
		if e.save != nil {
			reportWants[e.name] = e.save
		}
	}
	framingWants := map[string][]byte{}
	for _, e := range settle(expectations(&mForExpect, "framing", reportVerdicts, jsonDir)) {
		if e.save != nil {
			framingWants[e.name] = e.save
		}
	}
	hostileWants := map[string][]byte{}
	for _, e := range settle(expectations(&mForExpect, "json-hostile", reportVerdicts, jsonDir)) {
		if e.save != nil {
			hostileWants[e.name] = e.save
		}
	}
	checkWantSave := func(caseName, savePath string, engine []byte, wants map[string][]byte) {
		if engine == nil {
			return
		}
		want, ok := wants[caseName]
		if !ok {
			t.Errorf("%s: the harness holds no write-back wire expectation", caseName)
			return
		}
		if !bytes.Equal(want, engine) {
			t.Errorf("%s: the harness expectation is not the engine's write-back wire", caseName)
		}
	}

	for _, inst := range m.Instances {
		wireBytes, err := os.ReadFile(filepath.Join(root, inst.Wire))
		if err != nil || len(wireBytes) == 0 {
			t.Errorf("instance %s: missing wire %s", inst.Name, inst.Wire)
			continue
		}
		if inst.WireRefusal("go") != "" {
			continue
		}
		if !inst.NoText {
			if _, err := os.Stat(inst.JSON); err != nil {
				t.Errorf("readable instance %s missing canonical JSON %s", inst.Name, inst.JSON)
			}
		}
	}

	for _, rc := range m.Reports {
		verdict := reportVerdicts[rc.Name]
		switch verdict {
		case "read":
			engine := checkValueBytes(rc.Name, rc.Unit, rc.Root, strings.TrimPrefix(rc.Value, root))
			checkSaveWire(rc.Name, rc.Save, engine)
			checkWantSave(rc.Name, rc.Save, engine, reportWants)
		case "refused":
			if _, err := os.Stat(rc.Value); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused report %s still has value file %s", rc.Name, rc.Value)
			}
			if _, err := os.Stat(rc.Save); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused report %s still has write-back wire %s", rc.Name, rc.Save)
			}
		default:
			t.Errorf("report %s missing from reports.txt refuse/read set", rc.Name)
		}
	}

	framingValues := map[string]bool{}
	const framingDir = "testdata/conformance/tables/framing"
	for _, fc := range m.Framings {
		if fc.Refused {
			if fc.Value != "" {
				t.Errorf("refused framing %s still names value %s", fc.Name, fc.Value)
			}
			if _, err := os.Stat(fc.Save); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused framing %s still has write-back wire %s", fc.Name, fc.Save)
			}
		} else {
			if fc.Value == "" {
				t.Errorf("readable framing %s has no canonical value file", fc.Name)
			} else {
				framingValues[filepath.Base(fc.Value)] = true
				engine := checkValueBytes(fc.Name, fc.Unit, fc.Root, fc.Value)
				checkSaveWire(fc.Name, fc.Save, engine)
				checkWantSave(fc.Name, fc.Save, engine, framingWants)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, framingDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !framingValues[e.Name()] {
			t.Errorf("framing value %s is referenced by no read row", filepath.Join(framingDir, e.Name()))
		}
	}

	hostileValues := map[string]bool{}
	hostileDir := ""
	for _, h := range m.Hostiles {
		if hostileDir == "" {
			hostileDir = filepath.Dir(HostileValue(h.Tree))
		}
		if h.Refused {
			if h.Value != "" {
				t.Errorf("refused json-hostile %s still names value %s", h.Name, h.Value)
			}
			if _, err := os.Stat(filepath.Join(root, HostileValue(h.Tree))); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused json-hostile %s still has value file %s", h.Name, HostileValue(h.Tree))
			}
			if _, err := os.Stat(h.Save); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused json-hostile %s still has write-back wire %s", h.Name, h.Save)
			}
		} else {
			hostileValues[filepath.Base(HostileValue(h.Tree))] = true
			engine := checkValueBytes(h.Name, h.Unit, h.Root, h.Value)
			checkSaveWire(h.Name, h.Save, engine)
			checkWantSave(h.Name, h.Save, engine, hostileWants)
		}
	}
	if hostileDir != "" {
		entries, err := os.ReadDir(filepath.Join(root, hostileDir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".value.json") && !hostileValues[e.Name()] {
				t.Errorf("json-hostile value %s is referenced by no read row", filepath.Join(hostileDir, e.Name()))
			}
		}
	}

	for _, rc := range m.Retains {
		checkValueBytes(rc.Name, rc.Unit, rc.Root, "testdata/conformance/tables/retain-values/"+rc.Name+".json")
		if !rc.Short {
			if _, err := os.Stat(filepath.Join(root, rc.Saves)); err != nil {
				t.Errorf("retain %s missing canonical save wire %s", rc.Name, rc.Saves)
			}
		}
	}

	// Every readable report, framing and json-hostile row owes the canonical
	// write-back wire of the value its read leaves, not only the JSON text.
	const saveDir = "testdata/conformance/tables/save-wire"
	checkSaveWirePresent := func(caseName string) {
		path := filepath.Join(root, saveDir, caseName+".bin")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: missing canonical write-back wire %s: %v", caseName, path, err)
		}
	}
	for _, rc := range m.Reports {
		if reportVerdicts[rc.Name] == "read" {
			checkSaveWirePresent(rc.Name)
		}
	}
	for _, fc := range m.Framings {
		if !fc.Refused {
			checkSaveWirePresent(fc.Name)
		}
	}
	for _, h := range m.Hostiles {
		if !h.Refused {
			checkSaveWirePresent(h.Name)
		}
	}

	// 4. A leg that returns a partial value for a refused row fails.
	dir := t.TempDir()
	for _, surface := range []string{"report", "framing", "json-hostile"} {
		for _, partial := range []string{
			"refused\n{\"partial\":1}\n",
			"REFUSED\n{\"partial\":1}\n",
			"0,0,0,0,0,true,read\n{\"partial\":1}\n",
			"read\n{\"partial\":1}\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, "row"), []byte(partial), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreAnswer("go", surface, dir, expectation{name: "row", want: []byte("refused\n")}, Instance{}, r)
			if r.pass != 0 || len(r.failures) == 0 || slices.Contains(r.failures, "") {
				t.Errorf("%s: refused row with partial value %q did not fail: pass=%d failures=%v", surface, partial, r.pass, r.failures)
			}
		}
		// The verdict line is not the whole answer: a refused row that also
		// leaves a nonempty write-back wire beside it has returned a partial
		// value in its other form, and fails on every surface.
		rowPath := filepath.Join(dir, "row")
		if err := os.WriteFile(rowPath, []byte("refused\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		clean := &result{total: 1}
		scoreAnswer("go", surface, dir, expectation{name: "row", want: []byte("refused\n")}, Instance{}, clean)
		if clean.pass != 1 || len(clean.failures) != 0 {
			t.Errorf("%s: a clean refusal did not pass: pass=%d failures=%v", surface, clean.pass, clean.failures)
		}
		if err := os.WriteFile(rowPath+".wire", []byte{0x00, 0x02, 0x34, 0x56}, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("go", surface, dir, expectation{name: "row", want: []byte("refused\n")}, Instance{}, r)
		if r.pass != 0 || len(r.failures) == 0 || slices.Contains(r.failures, "") {
			t.Errorf("%s: refused row with a value-bearing .wire sidecar did not fail: pass=%d failures=%v", surface, r.pass, r.failures)
		}
		if err := os.Remove(rowPath + ".wire"); err != nil {
			t.Fatal(err)
		}
	}

	// 5. A leg that omits or corrupts the canonical write-back wire fails,
	// even when its JSON value is right: the wire is the other half of the
	// returned value and the half a text-only answer never exercises.
	wireDir := t.TempDir()
	const wireName = "row"
	wantSave := []byte{0x00, 0x02, 0x34, 0x56}
	e := expectation{name: wireName, want: []byte("read\n"), save: wantSave}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(wireDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	score := func() *result {
		t.Helper()
		r := &result{total: 1}
		scoreAnswer("go", "report", wireDir, e, Instance{}, r)
		return r
	}
	write(wireName, []byte("read\n"))
	write(wireName+".wire", wantSave)
	if r := score(); r.pass != 1 {
		t.Fatalf("the canonical write-back wire is red: %v", r.failures)
	}
	write(wireName+".wire", []byte{0xde, 0xad, 0xbe, 0xef})
	if r := score(); r.pass != 0 || len(r.failures) == 0 || !strings.Contains(strings.Join(r.failures, "\n"), ".wire") {
		t.Fatalf("a wrong write-back wire passed: pass=%d %v", r.pass, r.failures)
	}
	if err := os.Remove(filepath.Join(wireDir, wireName+".wire")); err != nil {
		t.Fatal(err)
	}
	if r := score(); r.pass != 0 || len(r.failures) == 0 || !strings.Contains(strings.Join(r.failures, "\n"), "write-back wire") {
		t.Fatalf("a missing write-back wire passed: pass=%d %v", r.pass, r.failures)
	}
}
