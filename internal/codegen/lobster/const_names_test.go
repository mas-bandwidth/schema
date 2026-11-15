package lobster

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

var (
	generatedDefLine  = regexp.MustCompile(`(?m)^[ \t]*def[ \t]+[A-Za-z_][A-Za-z0-9_]*\(([^)]*)\)`)
	generatedDeclLine = regexp.MustCompile(`(?m)^[ \t]+(?:let|var)[ \t]+([^=\n]+)`)
	generatedForLine  = regexp.MustCompile(`(?m)^[ \t]*for\([^)]*\)[ \t]*([^:\n]+):`)
)

// TestLobsterConstNamesNeverShadowGeneratedLocals declares a schema const for
// every parameter and local name the packet emitter writes, beside a type that
// uses every field kind, and requires the pinned Lobster to compile and run the
// module. A generated parameter or local a schema const can spell shadows it.
func TestLobsterConstNamesNeverShadowGeneratedLocals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		dir  string
		base string
		main string
	}{
		{"examples", filepath.Join("..", "..", "..", "examples"), "Wire", wireConstNamesProbe},
		{"wide", filepath.Join("..", "..", "..", "examples128"), "Ludicrous", wideConstNamesProbe},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runPacketConstNames(t, tc.dir, tc.base, tc.main)
		})
	}
}

func runPacketConstNames(t *testing.T, srcDir, base, main string) {
	t.Helper()
	baseline := t.TempDir()
	copySchemas(t, srcDir, baseline)
	files, err := Generate(unitFromDir(t, baseline))
	if err != nil {
		t.Fatal(err)
	}
	names := generatedLocals(t, string(files[base+".lobster"]))
	src, err := os.ReadFile(filepath.Join(srcDir, base+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	plain, underscore := splitConstNames(names)
	for i, set := range [][]string{plain, underscore} {
		if i == 1 && len(set) == 0 {
			continue
		}
		dir := t.TempDir()
		copySchemas(t, srcDir, dir)
		writeFile(t, dir, base+".schema", insertConsts(string(src), set))
		shadow, err := Generate(unitFromDir(t, dir))
		if err != nil {
			t.Fatalf("consts %v: %v", set, err)
		}
		run := t.TempDir()
		writeAll(t, run, shadow)
		writeFile(t, run, "main.lobster", main)
		if out := strings.TrimSpace(string(lobsterrun.Run(t, run, "main.lobster", nil))); out != "ok" {
			t.Fatalf("consts %v printed %q", set, out)
		}
	}
}

func copySchemas(t *testing.T, src, dst string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(src, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema files under %s: %v", src, err)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dst, filepath.Base(p), string(data))
	}
}

// generatedLocals is every parameter, let/var binding and loop variable the
// generated module declares. A name written here later is covered the moment
// the emitter writes it.
func generatedLocals(t *testing.T, src string) []string {
	t.Helper()
	names := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			names[name] = true
		}
	}
	for _, m := range generatedDefLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				name, _, _ := strings.Cut(fields[0], ":")
				add(name)
			}
		}
	}
	for _, m := range generatedDeclLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				name, _, _ := strings.Cut(fields[0], ":")
				add(name)
			}
		}
	}
	for _, m := range generatedForLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				add(fields[0])
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// splitConstNames keeps only names a schema declaration can take, split so a
// leading-underscore spelling and its bare twin never meet in one unit (the
// checker refuses that pair for another target's generated constant).
func splitConstNames(names []string) (plain, underscore []string) {
	for _, name := range names {
		if !schemaCanDeclare(name) {
			continue
		}
		if strings.HasPrefix(name, "_") {
			underscore = append(underscore, name)
		} else {
			plain = append(plain, name)
		}
	}
	sort.Strings(plain)
	sort.Strings(underscore)
	return plain, underscore
}

func schemaCanDeclare(name string) bool {
	toks, errs := scanner.Scan("generated", []byte(name))
	if len(errs) != 0 || len(toks) != 2 || toks[0].Kind != scanner.Ident || toks[0].Text != name || toks[1].Kind != scanner.EOF {
		return false
	}
	_, reserved := check.ReservedWords()[name]
	return !reserved
}

func insertConsts(src string, names []string) string {
	if len(names) == 0 {
		return src
	}
	var b strings.Builder
	inserted := false
	for _, line := range strings.SplitAfter(src, "\n") {
		b.WriteString(line)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "package ") {
			b.WriteString("\n")
			for _, name := range names {
				b.WriteString("const " + name + " = 0\n")
			}
			b.WriteString("\n")
			inserted = true
		}
	}
	return b.String()
}

const wireConstNamesProbe = `import .Wire
import .serialize

let v = example.NewProbeBits()
v.Small = 5
v.Boundary = 123456
v.Wide = 7
v.Sensor = 42
v.Nonce = 99
let w = serialize_write_stream(ensure_size("", 256, 0))
assert example.WriteProbeBits(w, v)
w.flush()
let r = serialize_read_stream_padded(substring(w.buffer, 0, w.bytes_processed()))
let out = example.NewProbeBits()
assert example.ReadProbeBits(r, out)
assert r.serialize_end()
assert out.Small == 5 and out.Boundary == 123456 and out.Wide == 7 and out.Sensor == 42 and out.Nonce == 99
print("ok")
`

const wideConstNamesProbe = `import .Ludicrous
import .serialize

let v = ludicrous.NewFixedProbe()
v.Ticks = 12345
let w = serialize_write_stream(ensure_size("", 256, 0))
assert ludicrous.WriteFixedProbe(w, v)
w.flush()
let r = serialize_read_stream_padded(substring(w.buffer, 0, w.bytes_processed()))
let out = ludicrous.NewFixedProbe()
assert ludicrous.ReadFixedProbe(r, out)
assert r.serialize_end()
assert out.Ticks == 12345
print("ok")
`
