// The unsigned fixed point gate (SPEC §4.3, ufixed(I, F)): serialize.lobster's
// fixed operations take a SIGNED Q format and refuse a declaration whose
// bounds the signed storage cannot hold, so a ufixed is passed as the widest
// signed declaration of the same fraction bits (fixedDeclaration, write.go);
// the wire depends on F and the raw bounds alone, so the bytes are the C++
// reference's. Two C++-pinned goldens hold it: bench_mixed, whose ufixed(8, 8)
// with max 250 is past a signed Q8.8's 127 (found by the bench leg,
// bench/lobster), and unsigned_probe, whose span's raw values fill uint64's
// high half. Each is read, its stream must end clean, and the value read must
// write the golden back byte for byte. The negative control restores the
// field's own (I, F) declaration and requires both rows to refuse.
package lobster

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

const ufixedProbe = `import .Bench
import .Ludicrous
import .serialize_stream

def done(s:serialize_write_stream) -> string:
    s.flush()
    return substring(s.buffer, 0, s.bytes_processed())

def stream_of(name:string) -> serialize_read_stream, string:
    let g = read_file(name)
    assert g
    return serialize_read_stream_padded(g), g

def bench_row():
    let r, g = stream_of("bench_mixed.bin")
    let v = bench.NewBenchMixed()
    if not bench.ReadBenchMixed(r, v) or not r.serialize_end():
        print("bench_mixed " + r.refusal_name())
    else:
        let w = serialize_write_stream(ensure_size("", 1024, 0))
        assert bench.WriteBenchMixed(w, v)
        print("bench_mixed " + (if done(w) == g: "ok" else: "differs"))

def unsigned_row():
    let r, g = stream_of("unsigned_probe.bin")
    let v = ludicrous.NewUnsignedProbe()
    if not ludicrous.ReadUnsignedProbe(r, v) or not r.serialize_end():
        print("unsigned_probe " + r.refusal_name())
    elif v.Span != -65536:
        print("unsigned_probe span " + string(v.Span))
    else:
        let w = serialize_write_stream(ensure_size("", 64, 0))
        assert ludicrous.WriteUnsignedProbe(w, v)
        print("unsigned_probe " + (if done(w) == g: "ok" else: "differs"))

bench_row()
unsigned_row()
`

func generateUfixed(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, src := range []string{
		filepath.Join("..", "..", "..", "bench", "corpus", "Bench.schema"),
		filepath.Join("..", "..", "..", "examples128", "Ludicrous.schema"),
	} {
		dir := t.TempDir()
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, filepath.Base(src), string(data))
		out, err := Generate(unitFromDir(t, dir))
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		maps.Copy(files, out)
	}
	return files
}

func runUfixed(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	writeAll(t, dir, files)
	inputs := map[string][]byte{}
	for _, name := range []string{"bench_mixed.bin", "unsigned_probe.bin"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	writeFile(t, dir, "main.lobster", ufixedProbe)
	return string(lobsterrun.Run(t, dir, "main.lobster", inputs))
}

func TestLobsterUnsignedFixedGoldens(t *testing.T) {
	got := runUfixed(t, generateUfixed(t))
	if want := "bench_mixed ok\nunsigned_probe ok\n"; got != want {
		t.Fatalf("ufixed goldens:\n%s\nwant:\n%s", got, want)
	}
}

// ufixedOwnDeclaration is the planted bug: every unsigned call site passes the
// field's own (I, F), as the emitter did before fixedDeclaration.
var ufixedOwnDeclaration = []struct{ from, to string }{
	{"serialize_fixed32(rv, 24, 8, 0, 250)", "serialize_fixed32(rv, 8, 8, 0, 250)"},
	{"serialize_fixed32(v.Ping, 24, 8, 0, 250)", "serialize_fixed32(v.Ping, 8, 8, 0, 250)"},
	{"rw, rok = stream.serialize_fixed128(rw, 112, 16, 0, 281474976710655) // the Q format's raw offset, unsigned (SPEC §4.3)", "rv, rok = stream.serialize_fixed64(rv, 48, 16, 0, 281474976710655)"},
}

func TestLobsterUnsignedFixedGoldensNegativeControl(t *testing.T) {
	files := generateUfixed(t)
	for _, p := range ufixedOwnDeclaration {
		name := "Bench.lobster"
		if strings.Contains(p.from, "281474976710655") {
			name = "Ludicrous.lobster"
		}
		src := string(files[name])
		if strings.Count(src, p.from) != 1 {
			t.Fatalf("negative-control anchor %q matched %d times in %s", p.from, strings.Count(src, p.from), name)
		}
		files[name] = []byte(strings.Replace(src, p.from, p.to, 1))
	}
	// the span read lands in rv now; its target reads the int, not rw.lo
	lud := string(files["Ludicrous.lobster"])
	re := regexp.MustCompile(`(serialize_fixed64\(rv, 48, 16, 0, 281474976710655\)\n    if not rok: return false\n    value\.Span = )rw\.lo`)
	if !re.MatchString(lud) {
		t.Fatalf("negative-control anchor for the span target did not match")
	}
	files["Ludicrous.lobster"] = []byte(re.ReplaceAllString(lud, "${1}rv"))
	got := runUfixed(t, files)
	if want := "bench_mixed value_out_of_range\nunsigned_probe value_out_of_range\n"; got != want {
		t.Fatalf("negative control: the planted (I, F) declarations must refuse both goldens, got:\n%s", got)
	}
}
