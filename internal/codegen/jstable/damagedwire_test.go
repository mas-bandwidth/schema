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

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestDamagedWireLoadsDefaultMalformedJavaScript pins the JavaScript leg's
// non-variable root Load and LoadMeasure on a wire whose framing is damaged
// before any field decodes (docs/SPEC-TABLES.md §3, §4): an empty wire and a
// truncated manifest instance wire must load a default root with
// report.malformed == true and all five evolution counters at zero, matching
// internal/tablewire.Decode.
func TestDamagedWireLoadsDefaultMalformedJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		unitDir string
		root    string
		wire    string
	}{
		{"tabledemo.RootConfig", "tables/examples", "RootConfig", "testdata/wire/tables/root_full.bin"},
		{"tblv1.Cfg", "test/tables/V1.schema", "Cfg", "testdata/wire/tables/v1_cfg.bin"},
		{"messagedemo.ToolMessage", "tables/messages", "ToolMessage", "testdata/wire/tables/message_open.bin"},
		{"widedemo.Stamp", "examples-wide", "Stamp", "testdata/wire/tables/wide_stamp.bin"},
		{"scalars.SimState", "tables/scalars", "SimState", "testdata/wire/tables/scalars_full.bin"},
		{"backenddemo.LoginRequest", "tables/backend", "LoginRequest", "testdata/wire/tables/login_full.bin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnitForMeasure(t, repo, tc.unitDir)
			m := tabletext.NewModel(u)
			def := m.Lookup(tc.root)
			if def == nil {
				t.Fatalf("no table %s in %s", tc.root, tc.unitDir)
			}

			fullWire, err := os.ReadFile(filepath.Join(repo, tc.wire))
			if err != nil {
				t.Fatal(err)
			}
			if len(fullWire) < 2 {
				t.Fatalf("fixture %s too short (%d bytes)", tc.wire, len(fullWire))
			}

			files, err := generateVariableFiles(u, nil)
			if err != nil {
				t.Fatalf("generateVariableFiles: %v", err)
			}
			work := t.TempDir()
			home := runtimeHome(u)
			module := filepath.Join(work, home+"Variable.js")
			if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
				t.Fatal(err)
			}
			data, exists := files[home+"Variable.js"]
			if !exists {
				t.Fatalf("no variable module %sVariable.js generated", home)
			}
			if err := os.WriteFile(module, data, 0o600); err != nil {
				t.Fatal(err)
			}

			wires := []struct {
				label string
				bytes []byte
			}{
				{"empty", []byte{}},
				{"truncated", fullWire[:len(fullWire)-1]},
			}

			for _, w := range wires {
				inst := m.New(def)
				var rep tabletext.Report
				ok, decErr := tablewire.Decode(m, inst, w.bytes, &rep)
				if decErr != nil {
					t.Fatalf("%s (%s): oracle Decode returned error: %v", tc.name, w.label, decErr)
				}
				wantMalformed := rep.Malformed || !ok
				wantSaved, err := tablewire.Encode(m, inst)
				if err != nil {
					t.Fatalf("%s (%s): oracle Encode failed: %v", tc.name, w.label, err)
				}
				wantSummary := fmt.Sprintf("loaded=true|measureOk=true|%d|%d|%d|%d|%d|malformed=%v",
					rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, wantMalformed)

				wirePath := filepath.Join(work, w.label+".wire")
				if err := os.WriteFile(wirePath, w.bytes, 0o600); err != nil {
					t.Fatal(err)
				}
				src := fmt.Sprintf(`
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(%q).href);
const wire = new Uint8Array(readFileSync(%q));
const measure = m.%sLoadMeasure(wire);
const rep = new m.TableVariableReport();
const v = m.%sLoad(wire, rep);
const loaded = v !== null;
const saved = loaded ? m.%sSave(v) : null;
const hex = saved !== null ? Buffer.from(saved).toString("hex") : "";
console.log("JS loaded=" + loaded + "|measureOk=" + (measure >= 0) + "|" + [rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate].join("|") + "|malformed=" + rep.malformed + "|hex=" + hex);
`, module, wirePath, tc.root, tc.root, tc.root)
				script := filepath.Join(work, "probe_"+w.label+".mjs")
				if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(node, script)
				cmd.Dir = work
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s (%s): node probe failed: %v\n%s", tc.name, w.label, err, out)
				}
				jsLine := ""
				for line := range strings.SplitSeq(string(out), "\n") {
					if after, ok0 := strings.CutPrefix(line, "JS "); ok0 {
						jsLine = after
					}
				}
				if jsLine == "" {
					t.Fatalf("%s (%s): no JS line in output:\n%s", tc.name, w.label, out)
				}
				parts := strings.Split(jsLine, "|hex=")
				if len(parts) != 2 {
					t.Fatalf("%s (%s): unexpected JS output %q", tc.name, w.label, jsLine)
				}
				gotSummary, gotHex := parts[0], strings.TrimSpace(parts[1])
				if gotSummary != wantSummary {
					t.Fatalf("%s (%s): JS=%q, want=%q", tc.name, w.label, gotSummary, wantSummary)
				}
				gotSaved, err := hex.DecodeString(gotHex)
				if err != nil {
					t.Fatalf("%s (%s): decode hex: %v", tc.name, w.label, err)
				}
				if !bytes.Equal(gotSaved, wantSaved) {
					t.Fatalf("%s (%s): saved default root differs (%d bytes vs %d bytes)", tc.name, w.label, len(gotSaved), len(wantSaved))
				}
			}
		})
	}
}
