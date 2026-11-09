package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCanonicalJSONWriterBytesAreOneAnswer holds every readable corpus instance
// to the Go reference writer's canonical text (docs/SPEC-TABLES.md §16.14):
// the bytes it writes are exactly the committed json/<name>.json golden, and
// writing the read-back value again yields the same bytes.
func TestCanonicalJSONWriterBytesAreOneAnswer(t *testing.T) {
	t.Parallel()

	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	for _, inst := range m.Instances {
		if inst.NoText {
			continue
		}
		inst := inst
		t.Run(inst.Name, func(t *testing.T) {
			t.Parallel()
			unit, err := u.get(inst.Unit)
			if err != nil {
				t.Fatal(err)
			}
			wirePath := inst.Wire
			if !filepath.IsAbs(wirePath) {
				wirePath = filepath.Join(root, wirePath)
			}
			wire, err := os.ReadFile(wirePath)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(inst.Root)
			if def == nil {
				t.Fatalf("%s: the unit declares no root %q", inst.Name, inst.Root)
			}
			val := model.New(def)
			var report tabletext.Report
			_, err = tablewire.Decode(model, val, wire, &report)
			if tablewire.Refused(err) {
				t.Fatalf("%s: the wire read refused the value: %v", inst.Name, err)
			}
			if err != nil {
				t.Fatalf("%s: the wire read failed: %v", inst.Name, err)
			}

			got1, err := model.Write(val)
			if err != nil {
				t.Fatalf("%s: write: %v", inst.Name, err)
			}
			goldenPath := inst.JSON
			if !filepath.IsAbs(goldenPath) {
				goldenPath = filepath.Join(root, goldenPath)
			}
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(oneText(got1), golden) {
				t.Fatalf("%s: the writer's bytes differ from the canonical JSON file:\nwriter:%q\ngolden:%q",
					inst.Name, got1, golden)
			}

			back := model.New(def)
			var backReport tabletext.Report
			if !model.Read(back, got1, &backReport) || backReport != (tabletext.Report{}) {
				t.Fatalf("%s: the text does not read back clean: %+v", inst.Name, backReport)
			}
			got2, err := model.Write(back)
			if err != nil {
				t.Fatalf("%s: second write: %v", inst.Name, err)
			}
			if !bytes.Equal(got2, got1) {
				t.Fatalf("%s: a second write of the read-back value differs from the first:\nfirst:%q\nsecond:%q",
					inst.Name, got1, got2)
			}
		})
	}
}

func checkCanonicalJSONWriterCodegenStrict(rustSrc, jsSrc, elixirSrc string) error {
	if strings.Contains(rustSrc, "from_utf16_lossy") {
		return fmt.Errorf("rusttable still replaces unpaired surrogates via from_utf16_lossy instead of refusing (§16.14 W0)")
	}
	if !strings.Contains(rustSrc, "fn json_escape_wide(") || !strings.Contains(rustSrc, "if !json_escape_wide(") {
		return fmt.Errorf("rusttable is missing json_escape_wide unpaired-surrogate refusal (§16.14 W0)")
	}
	if !strings.Contains(rustSrc, "text.parse::<f32>().map(|back| back as f64 == v).unwrap_or(false)") {
		return fmt.Errorf("rusttable is missing single-rounded float32 read-back in json_float (§16.14 W12)")
	}

	if strings.Contains(jsSrc, "\\ufffd") {
		return fmt.Errorf("jstable still replaces unpaired surrogates with U+FFFD instead of refusing (§16.14 W0)")
	}
	if !strings.Contains(jsSrc, `throw new RangeError("TableVariableNotText")`) {
		return fmt.Errorf("jstable is missing TableVariableNotText refusal on unpaired surrogate (§16.14 W0)")
	}
	if strings.Contains(jsSrc, "if (single ? Math.fround(back) === v : back === v) { return text; }") {
		return fmt.Errorf("jstable still narrows a double with Math.fround(back) in TableVariableFloatText (§16.14 W12)")
	}
	if !strings.Contains(jsSrc, "if (single ? f32OfToken(text, back) === v : back === v) { return text; }") {
		return fmt.Errorf("jstable is missing single-rounded f32OfToken read-back in TableVariableFloatText (§16.14 W12)")
	}

	if !strings.Contains(elixirSrc, "defp json_escape(<<c, _::binary>> = bin, acc) when c >= 0x80 do") {
		return fmt.Errorf("elixirtable json_escape is missing multibyte UTF-8 validation (§16.14 W0)")
	}
	if !strings.Contains(elixirSrc, "defp json_float_reads_back?(text, v, 4), do: json_float32_direct(text) == v") {
		return fmt.Errorf("elixirtable is missing single-rounded json_float32_direct read-back (§16.14 W12)")
	}
	return nil
}

func generateCanonicalJSONCodegenSources(t *testing.T) (rustSrc, jsSrc, elixirSrc string, jsFiles map[string][]byte, root string) {
	t.Helper()
	root = conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	tableUnit, err := u.get("tabledemo")
	if err != nil {
		t.Fatal(err)
	}
	wideUnit, err := u.get("widedemo")
	if err != nil {
		t.Fatal(err)
	}
	rustFiles, err := rusttable.Generate(wideUnit)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range rustFiles {
		rustSrc += string(b)
	}
	jsFiles, err = jstable.Generate(tableUnit)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range jsFiles {
		jsSrc += string(b)
	}
	elixirFiles, err := elixirtable.Generate(tableUnit)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range elixirFiles {
		elixirSrc += string(b)
	}
	return rustSrc, jsSrc, elixirSrc, jsFiles, root
}

// TestCanonicalJSONWriterCodegenStrict pins the §16.14 W0 (ill-formed text /
// unpaired surrogate refusal) and W12 (single-rounded float32 read-back)
// codegen invariants across the Rust, JavaScript, and Elixir table emitters.
func TestCanonicalJSONWriterCodegenStrict(t *testing.T) {
	t.Parallel()
	rustSrc, jsSrc, elixirSrc, jsFiles, root := generateCanonicalJSONCodegenSources(t)
	if err := checkCanonicalJSONWriterCodegenStrict(rustSrc, jsSrc, elixirSrc); err != nil {
		t.Fatal(err)
	}

	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var varModPath string
	for name, data := range jsFiles {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Variable.js") {
			varModPath = full
		}
	}
	if varModPath == "" {
		t.Fatal("jstable.Generate(tabledemo) emitted no *Variable.js file")
	}
	goldenPath := filepath.Join(root, "testdata/conformance/tables/json/text_float32_double_round.json")
	driverPath := filepath.Join(dir, "check.mjs")
	driver := fmt.Sprintf(`import { readFileSync } from "node:fs";
import * as M from %q;

const golden = readFileSync(%q, "utf8");
const rep = new M.TableVariableReport();
const v = M.ProfileConfigFromJson(golden, rep);
if (v === null) {
  throw new Error("ProfileConfigFromJson refused text_float32_double_round.json: " + JSON.stringify(rep));
}
const got = M.ProfileConfigToJson(v);
if (got !== golden) {
  throw new Error("ProfileConfigToJson mismatch:\ngot: " + JSON.stringify(got) + "\nwant: " + JSON.stringify(golden));
}
v.name = "a\uD800b";
const refused = M.ProfileConfigToJson(v);
if (refused !== null) {
  throw new Error("ProfileConfigToJson accepted unpaired surrogate: " + JSON.stringify(refused));
}
`, varModPath, goldenPath)
	if err := os.WriteFile(driverPath, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodeBin, driverPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node canonical JSON check failed: %v\n%s", err, out)
	}
}

func TestCanonicalJSONWriterCodegenStrictNegativeControl(t *testing.T) {
	t.Parallel()
	rustSrc, jsSrc, elixirSrc, _, _ := generateCanonicalJSONCodegenSources(t)

	if err := checkCanonicalJSONWriterCodegenStrict(
		strings.Replace(rustSrc, "if !json_escape_wide(", "let _ = String::from_utf16_lossy; if !(", 1),
		jsSrc,
		elixirSrc,
	); err == nil {
		t.Fatal("negative control failed: accepted rusttable from_utf16_lossy")
	}

	if err := checkCanonicalJSONWriterCodegenStrict(
		rustSrc,
		strings.Replace(jsSrc, "if (single ? f32OfToken(text, back) === v : back === v) { return text; }", "if (single ? Math.fround(back) === v : back === v) { return text; }", 1),
		elixirSrc,
	); err == nil {
		t.Fatal("negative control failed: accepted jstable Math.fround(back) double rounding")
	}

	if err := checkCanonicalJSONWriterCodegenStrict(
		rustSrc,
		jsSrc,
		strings.Replace(elixirSrc, "defp json_escape(<<c, _::binary>> = bin, acc) when c >= 0x80 do", "defp json_escape_disabled(bin, acc) do", 1),
	); err == nil {
		t.Fatal("negative control failed: accepted elixirtable without UTF-8 validation in json_escape")
	}
}
