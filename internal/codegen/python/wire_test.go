package python

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// repo is a path in the repository, from this package's directory.
func repo(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", ".."}, parts...)...)
}

// typeWireGoldens are the C++-pinned type-wire goldens of examples/, in the
// order the probe prints them (testdata/probe_wire.py; test/go/main.go builds
// the same instances).
var typeWireGoldens = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

type pin struct {
	name string
	run  func(testing.TB, string, string, map[string][]byte) []byte
}

// pins runs a probe under both pinned interpreters.
var pins = []pin{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}}

// probeInputs is a probe as main.py, and the named goldens of testdata/wire
// beside it.
func probeInputs(t *testing.T, probe string, goldens ...string) map[string][]byte {
	t.Helper()
	main, err := os.ReadFile(filepath.Join("testdata", probe))
	if err != nil {
		t.Fatal(err)
	}
	in := map[string][]byte{"main.py": main}
	for _, g := range goldens {
		b, err := os.ReadFile(repo("testdata", "wire", g+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		in[g+".bin"] = b
	}
	return in
}

func generated(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	files, err := Generate(pyrun.LoadUnit(t, paths...))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// wireLines is the wire probe's output: each golden's hex, and every FAILED
// line it printed.
func wireLines(t *testing.T, out []byte) (map[string][]byte, []string) {
	t.Helper()
	got := map[string][]byte{}
	var failed []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "FAILED: ") {
			failed = append(failed, line)
			continue
		}
		name, h, _ := strings.Cut(line, " ")
		b, err := hex.DecodeString(h)
		if err != nil {
			t.Fatalf("probe line %q is not a name and hex", line)
		}
		got[name] = b
	}
	return got, failed
}

func runWire(t *testing.T, p pin, files map[string][]byte) (map[string][]byte, []string) {
	t.Helper()
	dir := t.TempDir()
	pyrun.WriteTree(t, dir, files)
	py := pyrun.Interpreter(t)
	if p.name == "floor" {
		py = pyrun.Floor(t)
	}
	stdout, stderr, err := pyrun.Try(t, py, dir, "main.py", probeInputs(t, "probe_wire.py"))
	if err != nil && !strings.Contains(stdout, "FAILED: ") {
		t.Fatalf("the wire probe failed to run: %v\n%s\n%s", err, stderr, stdout)
	}
	return wireLines(t, []byte(stdout))
}

// TestPythonTypeWireMatchesTheGoldens generates examples/, builds every
// instance test/go/main.go builds, writes it under both pinned interpreters
// and byte-compares it with the C++-pinned testdata/wire/<name>.bin (the
// reference's bytes are the judge, never this leg's own). Each part is read
// back by the probe, writes back byte for byte, equals what was written where
// every field is exact, refuses when cut one byte short (past_end) and
// refuses one trailing byte (trailing_data).
func TestPythonTypeWireMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples"))
	for _, p := range pins {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got, failed := runWire(t, p, files)
			for _, f := range failed {
				t.Error(f)
			}
			for _, name := range typeWireGoldens {
				golden, err := os.ReadFile(repo("testdata", "wire", name+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got[name], golden) {
					t.Errorf("wire golden %s: python bytes %x != C++-pinned %x", name, got[name], golden)
				}
			}
		})
	}
}

// patchOnce replaces the one occurrence of anchor in a generated file, or
// fails t: a control whose sabotage patched nothing watches nothing.
func patchOnce(t *testing.T, files map[string][]byte, name, anchor, with string) {
	t.Helper()
	text := string(files[name])
	if n := strings.Count(text, anchor); n != 1 {
		t.Fatalf("the control's anchor %q occurs %d times in %s", anchor, n, name)
	}
	files[name] = []byte(strings.Replace(text, anchor, with, 1))
}

// TestPythonTypeWireWriteNegativeControl writes ShipCreate's has_flags bool
// twice and requires the shipcreate_flags golden comparison to go red.
func TestPythonTypeWireWriteNegativeControl(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples"))
	patchOnce(t, files, "example/Types.py",
		"        at = \"has_flags\"\n        R.value = v.has_flags\n        w_bool(R)\n",
		"        at = \"has_flags\"\n        R.value = v.has_flags\n        w_bool(R)\n        w_bool(R)\n")
	got, _ := runWire(t, pins[0], files)
	golden, err := os.ReadFile(repo("testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got["shipcreate_flags"], golden) {
		t.Fatal("negative control: the sabotaged write still produced the golden's bytes")
	}
}

// TestPythonTypeWireReadNegativeControl reads Trio's first bits(20) as 19
// bits. The writes stay the goldens' bytes, so only the read-back checks can
// go red, and they must, on the degenerate golden that holds Trio.
func TestPythonTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples"))
	text := string(files["example/Degenerate.py"])
	start := strings.Index(text, "def _s_r_Trio(")
	if start < 0 {
		t.Fatal("Degenerate.py has no _s_r_Trio")
	}
	anchor := "if not r_bits(R, 20):"
	i := strings.Index(text[start:], anchor)
	if i < 0 {
		t.Fatalf("_s_r_Trio has no %q", anchor)
	}
	i += start
	files["example/Degenerate.py"] = []byte(text[:i] + "if not r_bits(R, 19):" + text[i+len(anchor):])
	got, failed := runWire(t, pins[0], files)
	golden, err := os.ReadFile(repo("testdata", "wire", "degenerate.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["degenerate"], golden) {
		t.Fatal("the read sabotage moved the written bytes; the control would not isolate the read")
	}
	red := false
	for _, f := range failed {
		if strings.HasPrefix(f, "FAILED: degenerate: ") {
			red = true
		}
	}
	if !red {
		t.Fatalf("negative control: Trio read 19 bits and the degenerate read-back stayed green: %v", failed)
	}
}

// TestPythonLudicrousWire holds examples128 (fixed point as its raw scaled
// int, the 128-bit kinds as plain ints) to its four C++-pinned goldens under
// both pins, with the hostile-offset and truncation refusals, the untaken
// branch read back as zero, and the worst-case sizes.
func TestPythonLudicrousWire(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples128"))
	for _, p := range pins {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pyrun.WriteTree(t, dir, files)
			out := strings.TrimSpace(string(p.run(t, dir, "main.py", probeInputs(t, "probe_ludicrous.py",
				"ludicrous_state", "ludicrous_state_untargeted", "degenerate_probe", "unsigned_probe"))))
			if out != "OK" {
				t.Fatalf("ludicrous probe:\n%s", out)
			}
		})
	}
}

// refusals is every case of testdata/probe_refusals.py: a write outside its
// contract is InvalidValue naming the field path, a hostile read is Refused
// naming its reason (a diagnostic, held here so a change is seen).
var refusals = map[string]string{
	"ranged-int-above-max":  "TestData.a",
	"int-for-bool":          "TestData.g",
	"int-for-float":         "TestData.float_value",
	"float32-nan":           "TestData.float_value",
	"float32-overflow":      "TestData.float_value",
	"float64-infinity":      "TestData.double_value",
	"compressed-infinity":   "TestData.compressed_float_value",
	"string-nul":            "TestData.text",
	"string-lone-surrogate": "TestData.text",
	"string-too-long":       "TestData.text",
	"fixed-array-length":    "TestData.fixed_bytes",
	"counted-array-length":  "TestData.items",
	"array-element-range":   "TestData.items",
	"int8-above-range":      "TestData.int8_value",
	"uint64-negative":       "TestData.uint64_value",
	"bits-too-wide":         "TestData.d",
	"nested-wrong-class":    "ShipCreate.position",
	"enum-above-max":        "ShipCreate.ship_type",
	"nested-enum-path":      "ProbeArray.samples.weapon",
	"union-tag-above-max":   "ProbeCollider.shape.type",
	"bytes-too-long":        "Block.data",
	"bytes-wide-memoryview": "Block.data",
	"bytes-memoryview":      "WROTE 040061626364",
	"const-mismatch":        "value_out_of_range",
	"reserved-nonzero":      "value_out_of_range",
	"enum-above-max-read":   "value_out_of_range",
	"string-invalid-utf8":   "invalid_utf8",
	"string-embedded-nul":   "embedded_nul",
	"truncated":             "past_end",
	"trailing-byte":         "trailing_data",
	"empty":                 "past_end",
	"align-padding-nonzero": "nonzero_padding",
	"input-str":             "value_out_of_range",
	"input-wide-memoryview": "value_out_of_range",
	"input-bytearray":       "READ",
	"input-memoryview":      "READ",
}

// TestPythonRefusesOutOfContractValues runs every refusal case under both
// pins and holds each answer to the table.
func TestPythonRefusesOutOfContractValues(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples"))
	for _, p := range pins {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pyrun.WriteTree(t, dir, files)
			out := p.run(t, dir, "main.py", probeInputs(t, "probe_refusals.py", "probe_header", "shipcreate_flags", "chat"))
			seen := map[string]bool{}
			for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				name, answer, _ := strings.Cut(line, " ")
				want, ok := refusals[name]
				if !ok {
					t.Errorf("the probe answered a case the table does not hold: %q", line)
					continue
				}
				seen[name] = true
				if answer != want {
					t.Errorf("%s: got %q, want %q", name, answer, want)
				}
			}
			for name := range refusals {
				if !seen[name] {
					t.Errorf("the probe did not answer %s", name)
				}
			}
		})
	}
}

// TestPythonRefusalNegativeControl drops the float field's type guard from a
// generated TestData write, and requires the int-for-float case to stop
// refusing.
func TestPythonRefusalNegativeControl(t *testing.T) {
	t.Parallel()
	files := generated(t, repo("examples"))
	text := string(files["example/Wire.py"])
	start := strings.Index(text, "def _s_w_TestData(")
	anchor := "        if type(x"
	i := strings.Index(text[start:], "at = \"float_value\"")
	j := strings.Index(text[start+i:], anchor)
	if start < 0 || i < 0 || j < 0 {
		t.Fatal("the float_value guard is not where the control looks")
	}
	at := start + i + j
	files["example/Wire.py"] = []byte(text[:at] + "        if False and type(x" + text[at+len(anchor):])
	dir := t.TempDir()
	pyrun.WriteTree(t, dir, files)
	out := string(pyrun.Run(t, dir, "main.py", probeInputs(t, "probe_refusals.py", "probe_header", "shipcreate_flags", "chat")))
	if !strings.Contains(out, "int-for-float WROTE ") {
		t.Fatalf("negative control: without its guard, an int in a float field was still refused:\n%s", out)
	}
}
