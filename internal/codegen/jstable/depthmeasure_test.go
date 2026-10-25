package jstable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestDepthLoadMeasureCoversFramingJavaScript pins the JavaScript variable
// LoadMeasure (docs/SPEC-TABLES.md §6.5, §20.3) to internal/tablewire's
// FileRegionMeasure on mapdemo.Depth under the wire-fuzz cause (b) mutant: an
// arm's kind byte is rewritten so the reader sees it as unknown, while the
// oracle still counts the selected arm's payload by id. The measure must match
// byte for byte, and the JS load must then answer as the oracle.
func TestDepthLoadMeasureCoversFramingJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	// The kind #910 mutant of map_depth, run seed 24845619678, sha256
	// febb9f0270f92ebf1f9256959a4c93dd78956ab1f1fa5d0f7bec319e84fa3030.
	const wireHex = "01010d15020e110d010e030603040d07050421000000000000060e2e0d0215020e110d010e030601040d0705040b00000000000015020e110d010e030602040d070504160000000000000710190d010815020e110d010e030605040d07050437000000000000090f0a0015020e110d010e030607040d0705044d0000000000000b040900000000af5cca2119aa081a424f4f300d39841cec105b36194ac93dea0ce83094fde47c74a279448ee2e5b102fca1cea259641f5df15095f21f55708be145c2cb04da4fb5cc700519c056e731633ed695bbc2d5e9ea716f0f0182bf0b00000000000000"

	wireData, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/maps")
	root := findTable(t, u, "Depth")

	want, whole, refused := tablewire.FileRegionMeasure(u, root, wireData)
	if refused {
		t.Fatalf("oracle refused the wire for Depth")
	}
	if !whole {
		t.Fatalf("oracle did not read the whole wire for Depth")
	}

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}

	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	home := runtimeHome(u)
	script := filepath.Join(dir, "probe.mjs")
	probe := `
import { writeFileSync } from "node:fs";
import { DepthLoadMeasure, DepthLoad } from "./%[1]sVariable.js";
const wire = new Uint8Array(%[2]s);
const m = DepthLoadMeasure(wire).toString();
const report = { unknown:0, kindMismatch:0, widened:0, clamped:0, duplicate:0, malformed:false, reason:"" };
const v = DepthLoad(wire, report);
writeFileSync(process.argv[2], m + "\n" + (v === null ? "null" : "ok") + "\n");
`
	src := strings.ReplaceAll(probe, "%[1]s", home)
	src = strings.ReplaceAll(src, "%[2]s", jsu8array(wireData))
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.txt")
	cmd := exec.Command(node, script, outPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Fatalf("probe output too short: %q", string(out))
	}
	got, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil {
		t.Fatalf("could not parse LoadMeasure %q: %v", lines[0], err)
	}
	if got != want {
		t.Fatalf("Depth LoadMeasure: JS=%d, oracle FileRegionMeasure=%d", got, want)
	}
	if strings.TrimSpace(lines[1]) != "ok" {
		t.Fatalf("Depth Load did not answer as the oracle: %q", lines[1])
	}
}

func jsu8array(b []byte) string {
	const hextab = "0123456789abcdef"
	var sb strings.Builder
	sb.WriteString("[")
	for i, c := range b {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("0x")
		sb.WriteByte(hextab[c>>4])
		sb.WriteByte(hextab[c&0xf])
	}
	sb.WriteString("]")
	return sb.String()
}
