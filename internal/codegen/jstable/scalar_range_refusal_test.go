package jstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// scalarRangeFields is one field set at two declarations: the WRITER's wide
// one and this READER's narrow one. A writer value past the reader's
// declaration is out of the reader's range on every read.
func scalarRangeFields(writer bool) string {
	r, b, c, s, e, n := "uint16 | min = 0, max = 250", "bits(4)", "float32 | min = 0, max = 10, resolution = 0.5", "int8", "Color", "int32 | min = 0, max = 50"
	if writer {
		r, b, c, s, e, n = "uint16 | min = 0, max = 1000", "bits(8)", "float32 | min = 0, max = 100, resolution = 0.5", "int8", "WideColor", "int32 | min = 0, max = 100"
	}
	return fmt.Sprintf("{\n    keep uint32\n    r %s\n    b %s\n    c %s\n    s %s\n    e %s\n    n %s\n}\n", r, b, c, s, e, n)
}

func scalarRangeSchema(writer bool) string {
	fields := scalarRangeFields(writer)
	color := "enum Color { Red, Green }\n"
	if writer {
		color = "enum WideColor { Red, Green, Blue }\n"
	}
	return "package probe\n\n" + color + "\nfixed table M\n" + fields + "\ntable V\n" + fields
}

// TestJsMessageRangedScalarReadRefuses is the fast half: the generated message
// read of a ranged int32 holds the value to this reader's declared range by
// refusing out_of_range, and carries no clamp and no counter
// (docs/SPEC-TABLES.md §3.3, §4).
func TestJsMessageRangedScalarReadRefuses(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, scalarRangeSchema(false)))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := jsFn(string(files["ProbeTable.js"]), "MLoadMessageBody")
	if body == "" {
		t.Fatal("the generated module has no MLoadMessageBody")
	}
	if want := `if (x < 0 || x > 50) { TableMessageRefuse(r.Report, "out_of_range"); return false; }`; !strings.Contains(body, want) {
		t.Errorf("the message read of an int32 with range 0..50 lacks %q:\n%s", want, body)
	}
	for _, never := range []string{"Oor++", "x = 50"} {
		if strings.Contains(body, never) {
			t.Errorf("the message read still clamps or counts (%q):\n%s", never, body)
		}
	}
}

// TestJsScalarOutOfRangeRefusedAsTheOracle is one table over the JavaScript
// reader's scalar reads on the MESSAGE, VARIABLE and JSON forms
// (docs/SPEC-TABLES.md §3.3, §4, §16.2): a value past this reader's
// declaration — a ranged integer past its range, bits past their width, or a
// compressed float past its range — is refused as the Go engine refuses it (the verdict alone), never clamped to an edge and
// counted. Each case's verdict is the engine's, taken first; the in-range
// control reads, and an enum variant this reader cannot name is unknown,
// never out of range.
func TestJsScalarOutOfRangeRefusedAsTheOracle(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	reader := unitFrom(t, scalarRangeSchema(false))
	writer := unitFrom(t, scalarRangeSchema(true))
	rm, wm := tabletext.NewModel(reader), tabletext.NewModel(writer)

	files, err := Generate(reader)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	base, err := js.Generate(reader)
	if err != nil {
		t.Fatalf("js.Generate: %v", err)
	}
	dir := t.TempDir()
	for _, set := range []map[string][]byte{base, files} {
		for name, data := range set {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	announce := tablewire.Announce(writer)
	node := jsNode(t)

	for _, tc := range []struct {
		name, json string
		reason     string // the engine's verdict on every form: "" reads
	}{
		{"in_range", `{"r": 250, "b": 15, "c": 10, "s": -128, "e": "Green"}`, ""},
		{"ranged_integer_past_max", `{"r": 300}`, tablewire.ReasonOutOfRange},
		// the prior writer's lawful int32 (range 0..100) at this reader's 0..50
		{"prior_writer_int32_inside", `{"n": 40}`, ""},
		{"prior_writer_int32_past_max", `{"n": 75}`, tablewire.ReasonOutOfRange},
		{"bits_past_width", `{"b": 200}`, tablewire.ReasonOutOfRange},
		{"compressed_float_past_max", `{"c": 50}`, tablewire.ReasonOutOfRange},
		{"enum_variant_unnamed", `{"e": "Blue"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// the writer's values, set through the writer's own JSON read
			mInst, vInst := wm.New(wm.Lookup("M")), wm.New(wm.Lookup("V"))
			var wr tabletext.Report
			if !wm.Read(mInst, []byte(tc.json), &wr) || !wm.Read(vInst, []byte(tc.json), &wr) {
				t.Fatalf("the writer refused its own value: %+v", wr)
			}
			batch, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{mInst})
			if err != nil {
				t.Fatalf("EncodeMessages: %v", err)
			}
			wire, err := tablewire.EncodeVariable(wm, vInst)
			if err != nil {
				t.Fatalf("EncodeVariable: %v", err)
			}

			// THE ORACLE FIRST: the engine reads each form as this reader
			var vrep tabletext.Report
			voc := &tablewire.Vocabulary{}
			if err := voc.AnnounceRead(announce, &vrep); err != nil {
				t.Fatalf("the engine refused the announcement: %v", err)
			}
			var rep tabletext.Report
			_, ok, derr := tablewire.DecodeMessages(rm, []*tabletext.Instance{rm.New(rm.Lookup("M"))}, batch, voc, &rep)
			if got := tablewire.RefusalReason(derr); ok != (tc.reason == "") || got != tc.reason {
				t.Fatalf("message: the engine's verdict moved: ok=%t reason=%q, want %q (%v)", ok, got, tc.reason, derr)
			}
			rep = tabletext.Report{}
			ok, derr = tablewire.DecodeVariable(rm, rm.New(rm.Lookup("V")), wire, &rep)
			if got := tablewire.RefusalReason(derr); ok != (tc.reason == "") || got != tc.reason {
				t.Fatalf("variable: the engine's verdict moved: ok=%t reason=%q, want %q (%v)", ok, got, tc.reason, derr)
			}
			rep = tabletext.Report{}
			ok = rm.Read(rm.New(rm.Lookup("V")), []byte(tc.json), &rep)
			if ok != (tc.reason == "") || (!ok && !strings.HasPrefix(rep.Reason, tc.reason)) {
				t.Fatalf("json: the engine's verdict moved: ok=%t reason=%q, want %q", ok, rep.Reason, tc.reason)
			}

			// THEN JAVASCRIPT, held to it
			announcePath := filepath.Join(dir, tc.name+".announce")
			wirePath := filepath.Join(dir, tc.name+".wire")
			batchPath := filepath.Join(dir, tc.name+".batch")
			for p, b := range map[string][]byte{announcePath: announce, wirePath: wire, batchPath: batch} {
				if err := os.WriteFile(p, b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			probe := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { M, TableVocabulary, AnnounceRead, MLoadMessages } from "./ProbeTable.js";
import { VLoad, VFromJson, TableVariableReport } from "./ProbeVariable.js";
const voc = new TableVocabulary();
voc.Init(new Array(64));
if (!AnnounceRead(voc, new Uint8Array(readFileSync(%q)), {})) { throw new Error("announce refused"); }
const mrep = {};
const n = MLoadMessages([new M()], voc, new Uint8Array(readFileSync(%q)), mrep);
console.log("JS message loaded=" + (n === 1 && !mrep.Damaged));
console.log("diagnostic message reason=" + (mrep.Reason ?? "") + "|oor=" + (mrep.Oor ?? 0));
const vrep = new TableVariableReport();
const v = VLoad(new Uint8Array(readFileSync(%q)), vrep);
console.log("JS variable loaded=" + (v !== null));
console.log("diagnostic variable reason=" + (vrep.err() ?? ""));
const jrep = new TableVariableReport();
const j = VFromJson(%q, jrep);
console.log("JS json loaded=" + (j !== null));
console.log("diagnostic json reason=" + (jrep.err() ?? ""));
`, announcePath, batchPath, wirePath, tc.json)
			probePath := filepath.Join(dir, tc.name+".mjs")
			if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := testproc.Command(t, node, probePath)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node probe failed: %v\n%s", err, out)
			}
			// the verdict alone on every path: the reason is per-language detail
			// (rulings refusal-verdict-only)
			want := map[string]string{
				"message":  fmt.Sprintf("loaded=%t", tc.reason == ""),
				"variable": fmt.Sprintf("loaded=%t", tc.reason == ""),
				"json":     fmt.Sprintf("loaded=%t", tc.reason == ""),
			}
			got := map[string]string{}
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "JS "); ok {
					form, rest, _ := strings.Cut(after, " ")
					got[form] = rest
				}
			}
			for _, form := range []string{"message", "variable", "json"} {
				if got[form] != want[form] {
					t.Errorf("%s: JS=%q, want %q (the engine's verdict)\n%s", form, got[form], want[form], out)
				}
			}
		})
	}
}
