package mootable

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonProbeDir is a fresh directory holding the vendored serialize.moo, the
// text forms' runtime and main.moo, the probe a test runs under the pinned moo
// (docs/PORT-MOO.md, THE HARNESS). runtime overrides a runtime module by name,
// which is how a negative control plants its sabotage.
func jsonProbeDir(t *testing.T, main string, runtime map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	files := moo.Runtime()
	maps.Copy(files, Runtime())
	maps.Copy(files, runtime)
	files["main.moo"] = []byte(main)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// jsonProbe runs main under the pinned moo and returns its stdout as lines
// of hex, decoded. A probe prints each result through hexs (below).
func jsonProbe(t *testing.T, main string, inputs map[string][]byte, runtime map[string][]byte) []string {
	t.Helper()
	dir := jsonProbeDir(t, main, runtime)
	out := moorun.Run(t, dir, "main.moo", inputs)
	return decodeHexLines(t, out)
}

func decodeHexLines(t *testing.T, out []byte) []string {
	t.Helper()
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil
	}
	var lines []string
	for l := range strings.SplitSeq(text, "\n") {
		b, err := hex.DecodeString(l)
		if err != nil {
			t.Fatalf("probe output line %q is not hex: %v\n%s", l, err, out)
		}
		lines = append(lines, string(b))
	}
	return lines
}

// probePrelude is the moo every probe starts with: the runtime, the inputs
// and hexs(s), which prints one string as one hex line so any byte survives.
const probePrelude = `--!strict
local J = require("./json")
local inputs = require("./inputs")
local function hexs(s: string)
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i) :: number)
	end
	print(table.concat(parts))
end
local _ = J
local _ = inputs
local _ = hexs
`

// oracleModel compiles one schema source into the compiler's own text engine
// (internal/tabletext), the oracle every differential here is held to.
func oracleModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func oracleRoot(t *testing.T, m *tabletext.Model) *ir.Struct {
	t.Helper()
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("the probe declares no table Root")
	}
	return st
}

// oracleValueText is the text the engine writes for the instance's one field:
// the bytes after `"<key>": ` on the line that holds it.
func oracleValueText(t *testing.T, m *tabletext.Model, inst *tabletext.Instance) (string, bool) {
	t.Helper()
	out, err := m.Write(inst)
	if err != nil {
		return "", false
	}
	s := string(out)
	at := strings.Index(s, "\": ")
	if at < 0 {
		t.Fatalf("the engine's text %q holds no value", s)
	}
	s = s[at+3:]
	return s[:strings.IndexByte(s, '\n')], true
}
