// The type wire's read-side tests (docs/PORT-SQUIRREL.md, THE HARNESS): the
// golden test generates examples/*.schema into a t.TempDir, reads each
// C++-pinned wire golden (testdata/wire/<name>.bin) through the generated
// Read<Name> functions under the pinned interpreter, and holds every field of
// every value read against the instance test/go/main.go builds for it (the
// typeWireCases prelude the write side shares, in its want form: the untaken
// side of each branch zero, SPEC §5). The whole golden must be consumed, and
// each golden cut one byte short must refuse. The negative control patches the
// emitted read in memory so one uint32 field reads 31 bits and requires the
// field comparison, not a load error, to go red.
package squirrel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSquirrelTypeWireReadsTheGoldens reads every C++-pinned wire golden
// through the generated read and holds each field against the value the Go
// reference builds; each golden cut one byte short must refuse.
func TestSquirrelTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files := squirrelTypeWireFiles(t, root)
	// the read surface is wired into every module, for every type and union
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if name := readDeclName(d); name != "" {
				if !strings.Contains(string(files[f.Base+".nut"]), "_M.Read"+name+" <- function(stream, value)") {
					t.Errorf("%s.nut: missing Read%s", f.Base, name)
				}
			}
		}
	}
	results := readSquirrelTypeWire(t, root, u, files)
	for _, name := range goldenNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, ok := results[name]
			if !ok {
				t.Fatalf("the read probe printed no line for %s", name)
			}
			t.Logf("%s", r.line)
			if !r.green() {
				t.Errorf("%s: %s\n%s", name, r.line, strings.Join(r.mismatches, "\n"))
			}
		})
	}
}

// TestSquirrelTypeWireReadNegativeControl patches the emitted read in memory
// (one anchor, matched exactly once) so TestData's uint32_value reads 31 bits
// instead of 32, and requires the testdata golden's field comparison to go red
// while the probe still loads and runs, and every other golden stays green:
// the comparison would not be worth its green if a one-bit misread stayed
// invisible.
func TestSquirrelTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files := squirrelTypeWireFiles(t, root)
	const anchor = "if (!stream.serializeBits(_h, 32)) return false;\n  value.Uint32Value = _h.value;"
	count := 0
	for _, data := range files {
		count += strings.Count(string(data), anchor)
	}
	if count != 1 {
		t.Fatalf("the sabotage anchor %q matches %d times, want exactly 1", anchor, count)
	}
	for name, data := range files {
		if strings.Contains(string(data), anchor) {
			files[name] = []byte(strings.Replace(string(data), anchor,
				"if (!stream.serializeBits(_h, 31)) return false;\n  value.Uint32Value = _h.value;", 1))
		}
	}
	// readSquirrelTypeWire fails the test on a load or runtime error (sqrun
	// treats any stderr as a failure), so a red below is the comparison's
	results := readSquirrelTypeWire(t, root, u, files)
	for _, name := range goldenNames {
		r, ok := results[name]
		if !ok {
			t.Fatalf("the sabotaged probe printed no line for %s", name)
		}
		if name == "testdata" {
			if r.green() || len(r.mismatches) == 0 {
				t.Fatalf("the 31-bit uint32 read left the testdata comparison green: %s", r.line)
			}
			t.Logf("sabotaged testdata: %s; first mismatch: %s", r.line, r.mismatches[0])
			continue
		}
		if !r.green() {
			t.Errorf("%s went red under a TestData-only sabotage: %s", name, r.line)
		}
	}
}

// readResult is one golden's line from the read probe: whether every read
// returned true, how many leaf values were compared, the mismatches, whether
// the reads consumed the whole golden, and whether the one-byte-short copy
// refused.
type readResult struct {
	line         string
	read         bool
	leaves       int
	mismatches   []string
	consumed     bool
	truncRefused bool
}

func (r readResult) green() bool {
	return r.read && r.leaves > 0 && len(r.mismatches) == 0 && r.consumed && r.truncRefused
}

// readSquirrelTypeWire writes the generated files, the field-kind table and the
// read probe into a t.TempDir, runs it under the pinned interpreter with every
// golden as an input, and parses one result per golden.
func readSquirrelTypeWire(t *testing.T, root string, u *ir.Unit, files map[string][]byte) map[string]readResult {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := map[string][]byte{}
	for _, name := range goldenNames {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatalf("read wire golden %s: %v", name, err)
		}
		inputs[name] = data
	}
	probe := typeWireCases + fieldKinds(u) + typeWireReadProbe
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	results := map[string]readResult{}
	var cur *readResult
	var curName string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "  mismatch "); ok {
			if cur == nil {
				t.Fatalf("a mismatch line before any golden line: %q", line)
			}
			cur.mismatches = append(cur.mismatches, rest)
			results[curName] = *cur
			continue
		}
		var r readResult
		var name string
		var mismatches int
		if _, err := fmt.Sscanf(line, "%s read=%t leaves=%d mismatches=%d consumed=%t truncRefused=%t",
			&name, &r.read, &r.leaves, &mismatches, &r.consumed, &r.truncRefused); err != nil {
			t.Fatalf("read probe line %q: %v\n%s", line, err, out)
		}
		r.line = line
		results[name] = r
		cur, curName = &r, name
	}
	return results
}

// readDeclName is the name of a declaration that gets a Read<Name>.
func readDeclName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Struct:
		return d.Name
	case *ir.Union:
		return d.Name
	}
	return ""
}

// fieldKinds emits ::fieldKinds, type name -> member -> how a read value of
// that member compares against the built one: "f32" for every float32, raw or
// compressed (the built double rounded to float32, then exact: a raw float32
// rides its float32 bits, and a compressed one reconstructs in float32 per-op
// arithmetic, SPEC §4.3, so the want is the reconstructed value test/go/main.go
// checks with ==), and absent for everything else (exact, type and value).
// Union arms are keyed by their member name, like a struct's fields. A want
// leaf written { within = x, tol = t } (a compressed value main.go never pins
// exactly) compares within t instead.
func fieldKinds(u *ir.Unit) string {
	kind := func(f *ir.Field) string {
		if f == nil || f.Type.Kind != ir.TFloat32 {
			return ""
		}
		return "f32"
	}
	types := map[string]map[string]string{}
	for _, file := range u.Files {
		for _, d := range file.Decls {
			var fields []*ir.Field
			var name string
			switch d := d.(type) {
			case *ir.Struct:
				name, fields = d.Name, d.Fields
			case *ir.Union:
				name = d.Name
				for _, v := range d.Variants {
					if !v.Void() {
						fields = append(fields, v.F)
					}
				}
			default:
				continue
			}
			kinds := map[string]string{}
			for _, f := range fields {
				if k := kind(f); k != "" {
					kinds[squirrel.Member(f)] = k
				}
			}
			types[name] = kinds
		}
	}
	names := make([]string, 0, len(types))
	for n := range types {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\n::fieldKinds <- {\n")
	for _, n := range names {
		members := make([]string, 0, len(types[n]))
		for m := range types[n] {
			members = append(members, m)
		}
		sort.Strings(members)
		fmt.Fprintf(&b, "  %s = {", n)
		for _, m := range members {
			fmt.Fprintf(&b, " %s = %q,", m, types[n][m])
		}
		b.WriteString(" },\n")
	}
	b.WriteString("};\n")
	return b.String()
}

// typeWireReadProbe reads every golden through the generated Read<Name> calls
// of its case (typeWireCases), into a fresh instance per shape, and compares
// each value read, leaf by leaf, against the case's want instance. A "one" or
// "seq" case reads its shapes off one stream; a "parts" case reads each shape
// off its own slice, starting where the last one's bytes ended, as
// test/go/main.go's consume does. Each line is
//
//	<name> read=<bool> leaves=<n> mismatches=<n> consumed=<bool> truncRefused=<bool>
//
// followed by one "  mismatch <path>: got <x> want <y>" line per mismatch.
const typeWireReadProbe = `
local f32r = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return b.readn('f');
};

// class -> type name, so a nested instance finds its field kinds
local classNames = {};
foreach (mod in [Types, Wire, Enums, Degenerate, Clauses, Joins]) {
  foreach (k, v in mod) if (typeof v == "class") classNames[v] <- k;
}

local show = function(x) {
  if (typeof x == "float") return format("%.17g", x);
  if (x == null) return "null";
  return typeof x + ":" + x;
};

local leaves = 0;
local cmp = null;
cmp = function(path, got, want, kind, bad) {
  local tw = typeof want;
  if (tw == "instance") {
    if (typeof got != "instance" || got.getclass() != want.getclass()) {
      bad.append(path + ": got " + typeof got + ", want an instance of " + classNames[want.getclass()]);
      return;
    }
    local kinds = ::fieldKinds[classNames[want.getclass()]];
    foreach (k, mv in want.getclass()) {
      if (typeof mv == "function") continue;
      cmp(path + "." + k, got[k], want[k], k in kinds ? kinds[k] : null, bad);
    }
    return;
  }
  if (tw == "array" || tw == "blob") {
    if (typeof got != tw || got.len() != want.len()) {
      bad.append(path + ": got " + typeof got + " length " + (typeof got == tw ? got.len() : -1) + ", want " + tw + " length " + want.len());
      return;
    }
    for (local i = 0; i < want.len(); i++) cmp(path + "[" + i + "]", got[i], want[i], kind, bad);
    return;
  }
  leaves++;
  local ok;
  if (tw == "table" && "within" in want) {
    local d = typeof got == "float" ? got - want.within : 1.0e9;
    ok = d <= want.tol && -d <= want.tol;
    want = want.within;
  } else if (kind == "f32") {
    ok = typeof got == "float" && got == f32r(want.tofloat());
  } else {
    ok = typeof got == tw && got == want;
  }
  if (!ok) bad.append(path + ": got " + show(got) + " want " + show(want));
};

local sliceFrom = function(data, off) {
  local n = off < data.len() ? data.len() - off : 0;
  local out = blob(n);
  for (local i = 0; i < n; i++) out[i] = data[off + i];
  return out;
};

// readCase reads one golden's bytes through its case's shapes; bad collects
// the mismatches (null skips the comparison: the truncated read only asks
// whether it refused)
local readCase = function(c, data, bad) {
  local ok = true;
  local off = 0;
  local rs = serialize.ReadStream(data);
  foreach (i, it in c.items) {
    if (c.mode == "parts") rs = serialize.ReadStream(sliceFrom(data, off));
    local got = it.m[it.t]();
    local r = it.m["Read" + it.t](rs, got);
    if (r != true) ok = false;
    if (bad != null) cmp(c.name + "[" + i + "]." + it.t, got, it.v, null, bad);
    if (c.mode == "parts") off += rs.bytesProcessed();
    if (!ok) break;
  }
  if (c.mode != "parts") off = rs.bytesProcessed();
  return { ok = ok, consumed = ok && off == data.len() };
};

foreach (c in ::typeWireCases(true)) {
  local data = in_[c.name];
  local bad = [];
  leaves = 0;
  local full = readCase(c, data, bad);
  local cut = blob(data.len() - 1);
  for (local i = 0; i < cut.len(); i++) cut[i] = data[i];
  local trunc = readCase(c, cut, null);
  print(c.name + " read=" + full.ok + " leaves=" + leaves + " mismatches=" + bad.len() +
    " consumed=" + full.consumed + " truncRefused=" + !trunc.ok + "\n");
  foreach (m in bad) print("  mismatch " + m + "\n");
}
`
