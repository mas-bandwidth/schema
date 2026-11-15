package jstable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// skipTextAnnouncement is a form-1 announcement of one entry: a string(1)
// under an id the probe's Msg does not declare, so a body naming it takes the
// skip path. Its layout is the one messageform_skip_zero_width_test.go spells.
func skipTextAnnouncement() []byte {
	u64 := func(out []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(out, v) }
	entry := u64(nil, 0x123456789abcdef0)
	entry = append(entry, 12, 1) // kind 12, max 1
	body := append([]byte{6, byte(len(entry))}, entry...)
	out := []byte{1, 1, 9}
	out = u64(out, 0)
	out = append(out, 2, 14, byte(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = u64(out, 0xfffffffffffffffe)
	out = u64(out, 0xfffffffffffffffd)
	return u64(out, 2)
}

// TestJsMessageSkipHoldsTextToTheTextRule holds the JavaScript message reader
// to the engine on a string payload under an entry it does not declare
// (docs/SPEC-TABLES.md §3.3, §4): the skip path reads bytes it does not keep,
// and holds them to the rule a named read applies, so ill-formed UTF-8 or a
// zero byte is damage and the batch refuses. A well-formed payload is the
// control: it reads, with one unknown.
func TestJsMessageSkipHoldsTextToTheTextRule(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	const schemaSrc = `package probe

fixed table Msg
{
    keep uint32
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	baseFiles, err := js.Generate(u)
	if err != nil {
		t.Fatalf("js.Generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range baseFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeTable.js"), files["ProbeTable.js"], 0o600); err != nil {
		t.Fatal(err)
	}
	announce := skipTextAnnouncement()
	announcePath := filepath.Join(dir, "announce.bin")
	if err := os.WriteFile(announcePath, announce, 0o600); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("Msg")
	node := jsNode(t)

	for _, tc := range []struct {
		name     string
		payload  byte
		accepted bool
	}{
		{"well_formed", 'a', true},
		{"ill_formed_utf8", 0xff, false},
		{"zero_byte", 0x00, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ref 1 (one bit), count 1 (one bit), the pad to the byte, the
			// payload byte, then the zero reference that ends the body
			batch := []byte{2, 0, 0x03, tc.payload, 0x00}

			var vrep tabletext.Report
			v := &tablewire.Vocabulary{}
			if err := v.AnnounceRead(announce, &vrep); err != nil {
				t.Fatalf("the engine refused the announcement: %v", err)
			}
			var rep tabletext.Report
			_, ok, derr := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(def)}, batch, v, &rep)
			if derr != nil && !tablewire.ReadRefusal(derr) {
				t.Fatalf("engine DecodeMessages returned an error: %v", derr)
			}
			if ok != tc.accepted {
				t.Fatalf("the engine's verdict moved: accepted=%t, want %t (%v)", ok, tc.accepted, derr)
			}

			probe := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { Msg, TableVocabulary, AnnounceRead, MsgLoadMessages } from "./ProbeTable.js";
const v = new TableVocabulary();
v.Init(new Array(16));
if (!AnnounceRead(v, new Uint8Array(readFileSync(%q)), {})) { throw new Error("announce refused"); }
const rep = {};
const n = MsgLoadMessages([new Msg()], v, new Uint8Array(%s), rep);
console.log("JS loaded=" + (n === 1 && !rep.Damaged) + "|unknown=" + (n === 1 ? rep.Unknown : 0));
`, announcePath, jsByteList(batch))
			probePath := filepath.Join(dir, "probe_"+tc.name+".mjs")
			if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, probePath)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node probe failed: %v\n%s", err, out)
			}
			want := "loaded=false|unknown=0"
			if tc.accepted {
				want = "loaded=true|unknown=1"
			}
			got := ""
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "JS "); ok {
					got = after
				}
			}
			if got != want {
				t.Fatalf("JS=%q, want %q\n%s", got, want, out)
			}
		})
	}
}
