package goosetable_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestGooseRetainingFuzzDriver(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the retaining fuzz driver)")
	out := gooseRetainingFuzzProbe(t, nil)
	checkGooseRetainingReplies(t, out, 9)
}

func TestGooseRetainingFuzzNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the retaining fuzz driver)")
	checkGooseRetainingReplies(t, gooseRetainingFuzzProbe(t, nil), 9)
	// A real runtime counter defect must survive the driver and reach its reply.
	src := string(goosetable.Runtime()["wire_retain.goose"])
	const anchor = "    c.report.retained += 1;"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("retained counter anchor must occur once")
	}
	src = strings.Replace(src, anchor, "    c.report.retained += 0;", 1)
	out := gooseRetainingFuzzProbe(t, map[string][]byte{"wire_retain.goose": []byte(src)})
	// Bytes still match; the retained count alone exposes this mutation.
	checkGooseRetainingReplies(t, out, 0)
}

func checkGooseRetainingReplies(t *testing.T, out []byte, retained uint32) {
	t.Helper()
	if len(out) < 1 || out[0] != 1 {
		t.Fatalf("retaining roster declined: %x", out)
	}
	out = out[1:]
	for _, name := range []string{"clean", "evolution", "short", "form"} {
		if len(out) < 47 {
			t.Fatalf("%s: truncated protocol reply", name)
		}
		h := out[:47]
		out = out[47:]
		n := int64(binary.LittleEndian.Uint64(h[39:]))
		if name == "clean" {
			want := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			if h[0] != 1 || !bytes.Equal(h[1:23], make([]byte, 22)) || !bytes.Equal(h[31:39], make([]byte, 8)) {
				t.Errorf("clean: incorrect reply: %x", h)
			}
			if n != int64(len(want)) || n > int64(len(out)) {
				t.Fatalf("clean: saved length %d, want %d", n, len(want))
			}
			if !bytes.Equal(out[:int(n)], want) {
				t.Error("clean: default value wire changed")
			}
			out = out[int(n):]
		} else if name == "evolution" {
			want, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt1_save.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if h[0] != 1 || h[21] != 0 || h[22] != 0 || binary.LittleEndian.Uint32(h[1:]) != 11 || binary.LittleEndian.Uint32(h[31:]) != retained || binary.LittleEndian.Uint32(h[35:]) != 2 {
				t.Errorf("evolution: incorrect loaded/report/retention reply: %x", h)
			}
			if n != int64(len(want)) || n > int64(len(out)) {
				t.Fatalf("evolution: saved length %d, want %d", n, len(want))
			}
			if !bytes.Equal(out[:int(n)], want) {
				t.Error("evolution: retained save differs from committed oracle bytes")
			}
			out = out[int(n):]
		} else {
			// Damage and unsupported forms expose no value; counters on damage are undefined.
			if h[0] != 0 || n != -1 {
				t.Errorf("%s: refusal exposed a value: %x", name, h)
			}
			if name == "short" && h[21] != 1 {
				t.Error("short: missing malformed verdict")
			}
			if name == "form" && h[22] != 1 {
				t.Error("form: missing refused verdict")
			}
		}
	}
	if len(out) != 0 {
		t.Fatalf("unexpected protocol tail: %x", out)
	}
}

func gooseRetainingFuzzProbe(t *testing.T, override map[string][]byte) []byte {
	t.Helper()
	clean, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return gooseRetainingDriverFrames(t, "tblrt1", "Node", [][]byte{{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, clean, {1}, {0}}, override)
}

func gooseRetainingDriverFrames(t *testing.T, key, rootName string, frames [][]byte, override map[string][]byte) []byte {
	t.Helper()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, key, c.units[key], override)
	delete(files, "main.goose")
	for _, name := range []string{"wirefuzz.goose", "wirefuzz.build", "wirefuzz_io.c"} {
		b, err := os.ReadFile(filepath.Join(root, "test/conformance/goose", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatal(why)
	}
	build := exec.Command(goose, "build", "-O2", "wirefuzz.goose", "-o", "wirefuzz")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build driver: %v\n%s", err, out)
	}
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(1))
	for _, name := range []string{key, rootName} {
		binary.Write(&input, binary.LittleEndian, uint16(len(name)))
		input.WriteString(name)
	}
	input.Write([]byte{1, 1}) // file form, retaining roster entry
	for _, b := range frames {
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(b)))
		input.Write(b)
	}
	cmd := exec.Command(filepath.Join(dir, "wirefuzz"), key, "builder")
	cmd.Dir = dir
	cmd.Stdin = &input
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run driver: %v\n%s", err, stderr.Bytes())
	}
	return stdout.Bytes()
}

// This committed legacy vector contains a repeated names field. The current
// oracle replaces that map (retainmaprepeat_test.go); the upcoming duplicate
// field damage contract must revise this success expectation explicitly.
func TestGooseRetainingMapReplacement(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the retaining fuzz driver)")
	gooseRetainingMapReplay(t, nil, 0)
}

func TestGooseRetainingMapReplacementNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the retaining fuzz driver)")
	baseline := gooseRetainingMapReplay(t, nil, 0)
	src := string(goosetable.Runtime()["wire_load.goose"])
	const anchor = "        if c.retaining && c.vals[slot].made { forget_rec(c, slot); }\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("map retirement anchor must occur once")
	}
	src = strings.Replace(src, anchor, "", 1)
	mutant := gooseRetainingMapReplay(t, map[string][]byte{"wire_load.goose": []byte(src)}, 1)
	// Only retain_lost may change. The loaded value and saved bytes agree.
	binary.LittleEndian.PutUint32(baseline[36:], 1)
	if !bytes.Equal(baseline, mutant) {
		t.Fatal("map retirement sabotage changed more than retain_lost")
	}
}

func gooseRetainingMapReplay(t *testing.T, override map[string][]byte, lost uint32) []byte {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out := gooseRetainingDriverFrames(t, "mapdemo", "Text", [][]byte{wire}, override)
	if len(out) < 48 || out[0] != 1 {
		t.Fatalf("map retaining protocol: %x", out)
	}
	h := out[1:48]
	if h[0] != 1 || h[21] != 0 || h[22] != 0 || binary.LittleEndian.Uint32(h[31:]) != 2 || binary.LittleEndian.Uint32(h[35:]) != lost {
		t.Fatalf("map replacement: incorrect loaded/retention reply, want retained=2 lost=%d: %x", lost, h)
	}
	if n := int64(binary.LittleEndian.Uint64(h[39:])); n != int64(len(out)-48) || n != 72 {
		t.Fatalf("map replacement saved length %d", n)
	}
	return out
}
