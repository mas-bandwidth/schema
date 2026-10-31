package squirrel_test

// THE PACKET HALF OF WIDE TEXT (SPEC §4.12): every `operation wstring` vector
// of the shared serialize corpus (testdata/conformance/text/wstring.txt) read
// through a generated `wstring(N)` type, N = buffer_size - 1, and every
// canonical vector written back. A read must accept exactly the vectors the
// corpus accepts, landing the stated code units, and refuse the others (the
// length, a group above 0xFFFF, a zero group, an unpaired surrogate,
// exhaustion); a canonical write must produce the vector's bytes. A
// buffer_size below 3 is a wstring(N) the language refuses (N below 2), so
// those vectors have no type to read through and are counted, not run.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

type wideVector struct {
	name      string
	size      int
	bytes     []byte
	refused   bool
	units     []string
	canonical bool
}

func wideVectors(t *testing.T, path string) []wideVector {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []wideVector
	var cur *wideVector
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case line == "operation wstring":
			flush()
			cur = &wideVector{}
		case cur == nil:
			t.Fatalf("%s: %q outside an operation record", path, line)
		case strings.HasPrefix(line, "name "):
			cur.name = strings.TrimPrefix(line, "name ")
		case strings.HasPrefix(line, "param buffer_size = "):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "param buffer_size = "))
			if err != nil {
				t.Fatal(err)
			}
			cur.size = n
		case line == "bytes" || strings.HasPrefix(line, "bytes "):
			b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimPrefix(line, "bytes"), " ", ""))
			if err != nil {
				t.Fatalf("%s: %v", cur.name, err)
			}
			cur.bytes = b
		case line == "expect refused":
			cur.refused = true
		case strings.HasPrefix(line, "expect value ="):
			cur.units = strings.Fields(strings.TrimPrefix(line, "expect value ="))
		case line == "writer canonical":
			cur.canonical = true
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSquirrelTypeWireWideTextVectors(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	vectors := wideVectors(t, filepath.Join(root, "testdata", "conformance", "text", "wstring.txt"))
	sizes := map[int]bool{}
	var run []wideVector
	skipped := 0
	for _, v := range vectors {
		if v.size < 3 {
			skipped++
			continue
		}
		sizes[v.size-1] = true
		run = append(run, v)
	}
	if len(run) < 20 {
		t.Fatalf("the corpus gave %d runnable wstring vectors; it holds more than twenty", len(run))
	}
	var bounds []int
	for n := range sizes {
		bounds = append(bounds, n)
	}
	sort.Ints(bounds)
	var src strings.Builder
	src.WriteString("package widevec\n")
	for _, n := range bounds {
		fmt.Fprintf(&src, "type W%d\n{\n    text wstring(%d)\n}\n", n, n)
	}
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "WideVec.schema")
	if err := os.WriteFile(schemaPath, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(loadUnit(t, schemaPath))
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, squirrel.Serialize())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString(`local schema = dofile("schema_module.nut", true);
local serialize = dofile("serialize.nut", true);
local in_ = dofile("inputs.nut", true);
local M = schema.require("WideVec");
local probe = function(name, t, data, units) {
  local v = M[t]();
  local ok = M["Read" + t](serialize.ReadStream(data), v);
  local s = name + (ok ? " read" : " refused");
  if (ok) for (local i = 0; i < v.TextLength; i++) s += format(" %04X", v.Text[i]);
  print(s + "\n");
  if (units == null) return;
  local w = M[t]();
  foreach (i, u in units) w.Text[i] = u;
  w.TextLength = units.len();
  local ws = serialize.WriteStream(blob(256));
  if (!M["Write" + t](ws, w)) { print(name + " write-failed\n"); return; }
  ws.flush();
  print(name + " wrote ");
  ::hexline(ws.data());
};
`)
	for _, v := range run {
		inputs[v.name] = v.bytes
		units := "null"
		if v.canonical {
			parts := make([]string, len(v.units))
			for i, u := range v.units {
				parts[i] = "0x" + u
			}
			units = "[" + strings.Join(parts, ", ") + "]"
		}
		fmt.Fprintf(&p, "probe(%q, \"W%d\", in_[%q], %s);\n", v.name, v.size-1, v.name, units)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	wrote := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		if hexText, ok := strings.CutPrefix(rest, "wrote "); ok {
			wrote[name] = strings.ToLower(strings.TrimSpace(hexText))
			continue
		}
		if rest == "write-failed" {
			wrote[name] = rest
			continue
		}
		got[name] = rest
	}
	written := 0
	for _, v := range run {
		want := "refused"
		if !v.refused {
			want = strings.TrimSpace("read " + strings.Join(v.units, " "))
		}
		if got[v.name] != want {
			t.Errorf("%s: read gave %q, the corpus says %q", v.name, got[v.name], want)
		}
		if v.canonical {
			written++
			if w := hex.EncodeToString(v.bytes); wrote[v.name] != w {
				t.Errorf("%s: write gave %q, the corpus says %q", v.name, wrote[v.name], w)
			}
		}
	}
	t.Logf("%d wstring vectors read (%d written back), %d below the wstring(2) floor not run", len(run), written, skipped)
}
