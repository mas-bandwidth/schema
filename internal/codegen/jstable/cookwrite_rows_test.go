package jstable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type cookWriteManifestRow struct {
	instance string
	unitKey  string
	root     string
	wireFile string
	little   string
	big      string
}

func readCookWriteManifest(t *testing.T, repo string) (map[string][]string, []cookWriteManifestRow) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type instInfo struct {
		unitKey  string
		root     string
		wireFile string
	}
	units := map[string][]string{}
	instances := map[string]instInfo{}
	var rows []cookWriteManifestRow
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "unit":
			if len(fields) < 3 {
				t.Fatalf("malformed unit line: %q", line)
			}
			paths := make([]string, len(fields)-2)
			for i, p := range fields[2:] {
				paths[i] = filepath.Join(repo, p)
			}
			units[fields[1]] = paths
		case "instance":
			if len(fields) < 5 {
				t.Fatalf("malformed instance line: %q", line)
			}
			instances[fields[1]] = instInfo{
				unitKey:  fields[2],
				root:     fields[3],
				wireFile: filepath.Join(repo, fields[4]),
			}
		case "cook-write":
			if len(fields) != 4 {
				t.Fatalf("malformed cook-write line: %q", line)
			}
			inst, ok := instances[fields[1]]
			if !ok {
				t.Fatalf("cook-write %q names unknown instance", fields[1])
			}
			rows = append(rows, cookWriteManifestRow{
				instance: fields[1],
				unitKey:  inst.unitKey,
				root:     inst.root,
				wireFile: inst.wireFile,
				little:   filepath.Join(repo, fields[2]),
				big:      filepath.Join(repo, fields[3]),
			})
		}
	}
	return units, rows
}

// TestCookWriteAnswersEveryCookWriteRowJavaScript holds every cook-write row in
// testdata/conformance/tables/MANIFEST.txt byte for byte against the committed
// little-endian and big-endian golden files (docs/SPEC-TABLES.md §7.6).
func TestCookWriteAnswersEveryCookWriteRowJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	unitPaths, rows := readCookWriteManifest(t, repo)
	if len(rows) == 0 {
		t.Fatal("no cook-write rows found in MANIFEST.txt")
	}

	dir := t.TempDir()
	unitModules := map[string]string{}
	for _, row := range rows {
		if _, done := unitModules[row.unitKey]; done {
			continue
		}
		paths, ok := unitPaths[row.unitKey]
		if !ok {
			t.Fatalf("unknown unit %q", row.unitKey)
		}
		u := loadUnitPaths(t, paths)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate(%s): %v", row.unitKey, err)
		}
		unitDir := filepath.Join(dir, row.unitKey)
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(unitDir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		home := runtimeHome(u) + "Variable.js"
		if _, ok := files[home]; !ok {
			t.Fatalf("unit %s did not emit %s", row.unitKey, home)
		}
		unitModules[row.unitKey] = filepath.ToSlash(filepath.Join(unitDir, home))
	}

	var script strings.Builder
	script.WriteString("import * as fs from \"node:fs\";\n")
	unitIdx := map[string]int{}
	nextIdx := 0
	for unitKey, modPath := range unitModules {
		idx := nextIdx
		nextIdx++
		unitIdx[unitKey] = idx
		fmt.Fprintf(&script, "import * as u%d from %q;\n", idx, modPath)
	}
	script.WriteString("const cases = [\n")
	for _, row := range rows {
		fmt.Fprintf(&script, "  { name: %q, mod: u%d, root: %q, wire: %q },\n",
			row.instance, unitIdx[row.unitKey], row.root, filepath.ToSlash(row.wireFile))
	}
	script.WriteString(`];
for (const c of cases) {
  const load = c.mod[c.root + "Load"];
  const measure = c.mod[c.root + "CookMeasure"];
  const cookFrom = c.mod[c.root + "CookFrom"];
  if (typeof load !== "function" || typeof measure !== "function" || typeof cookFrom !== "function") {
    console.log(c.name + " MISSING");
    continue;
  }
  const wire = new Uint8Array(fs.readFileSync(c.wire));
  const inst = load(wire);
  if (inst === null) {
    console.log(c.name + " LOAD_FAILED");
    continue;
  }
  const n = measure(inst);
  if (!(n > 0)) {
    console.log(c.name + " MEASURE_FAILED " + n);
    continue;
  }
  const short = new Uint8Array(n - 1);
  short.fill(0xa5);
  if (cookFrom(inst, short, 1) !== false || !short.every((b) => b === 0xa5)) {
    console.log(c.name + " SHORT_FAILED");
    continue;
  }
  const le = new Uint8Array(n);
  const be = new Uint8Array(n);
  if (!cookFrom(inst, le, 1) || !cookFrom(inst, be, 2)) {
    console.log(c.name + " COOK_FAILED");
    continue;
  }
  console.log(c.name + " OK " + Buffer.from(le).toString("hex") + " " + Buffer.from(be).toString("hex"));
}
`)
	scriptPath := filepath.Join(dir, "run.mjs")
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, scriptPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	results := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 {
			results[parts[0]] = parts[1]
		}
	}

	for _, row := range rows {
		row := row
		t.Run(row.instance, func(t *testing.T) {
			res, ok := results[row.instance]
			if !ok {
				t.Fatalf("%s: no output from probe", row.instance)
			}
			if !strings.HasPrefix(res, "OK ") {
				t.Fatalf("%s (%s.%s): %s", row.instance, row.unitKey, row.root, res)
			}
			parts := strings.Split(res, " ")
			if len(parts) != 3 {
				t.Fatalf("%s: unexpected result %q", row.instance, res)
			}
			gotLE, err := hex.DecodeString(parts[1])
			if err != nil {
				t.Fatal(err)
			}
			gotBE, err := hex.DecodeString(parts[2])
			if err != nil {
				t.Fatal(err)
			}
			wantLE, err := os.ReadFile(row.little)
			if err != nil {
				t.Fatal(err)
			}
			wantBE, err := os.ReadFile(row.big)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotLE, wantLE) {
				t.Fatalf("%s little-endian cook mismatch: got %d bytes, want %d bytes", row.instance, len(gotLE), len(wantLE))
			}
			if !bytes.Equal(gotBE, wantBE) {
				t.Fatalf("%s big-endian cook mismatch: got %d bytes, want %d bytes", row.instance, len(gotBE), len(wantBE))
			}
		})
	}
}
