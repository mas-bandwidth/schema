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
	for i := range mt.NumField() {
		sf := mt.Field(i)
		if retiredFields[sf.Name] {
			t.Errorf("Manifest still carries retired field %s", sf.Name)
		}
		if sf.Type.Kind() == reflect.Slice && sf.Type.Elem().Kind() == reflect.Struct {
			et := sf.Type.Elem()
			for j := range et.NumField() {
				ef := et.Field(j)
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

	checkValueBytes := func(caseName, unitKey, rootName, valuePath string) {
		rawText, err := os.ReadFile(filepath.Join(root, valuePath))
		if err != nil {
			t.Errorf("%s: missing canonical value file %s: %v", caseName, valuePath, err)
			return
		}
		unit, err := u.get(unitKey)
		if err != nil {
			t.Errorf("%s: unit %s: %v", caseName, unitKey, err)
			return
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(rootName)
		if def == nil {
			t.Errorf("%s: missing root %s.%s", caseName, unitKey, rootName)
			return
		}
		inst := model.New(def)
		var rep tabletext.Report
		if !strictReadJSON(model, inst, rawText, &rep) {
			t.Errorf("%s: canonical value %s did not read cleanly", caseName, valuePath)
			return
		}
		textBack, err := model.Write(inst)
		if err != nil || !bytes.Equal(oneText(textBack), rawText) {
			t.Errorf("%s: value file %s is not canonical JSON/text", caseName, valuePath)
		}
		wireBack, err := tablewire.Encode(model, inst)
		if err != nil || len(wireBack) == 0 {
			t.Errorf("%s: canonical value %s failed wire encode: %v", caseName, valuePath, err)
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
			checkValueBytes(rc.Name, rc.Unit, rc.Root, strings.TrimPrefix(rc.Value, root))
		case "refused":
			if _, err := os.Stat(rc.Value); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused report %s still has value file %s", rc.Name, rc.Value)
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
		} else {
			if fc.Value == "" {
				t.Errorf("readable framing %s has no canonical value file", fc.Name)
			} else {
				framingValues[filepath.Base(fc.Value)] = true
				checkValueBytes(fc.Name, fc.Unit, fc.Root, fc.Value)
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
		} else {
			hostileValues[filepath.Base(HostileValue(h.Tree))] = true
			checkValueBytes(h.Name, h.Unit, h.Root, h.Value)
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
	}
}
