package javatable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A bits(N) PAST 2^N - 1 IS REFUSED in the fixed form, and the comparison is
// UNSIGNED (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.3): a bits(N) of 32 bits or
// fewer is stored in four bytes and one of 33 to 63 bits in eight, so a
// storage value with its top bit set is far past 2^N - 1 and the whole load
// refuses, exactly as a value of 2^N does. The record holding 2^N - 1 in each
// field reads. Only the verdict is compared.
const javaBitsBoundSchema = "package bitsbound\n\nfixed table T\n{\n    small bits(5)\n    wide bits(40)\n}\n"

const javaBitsBoundProbe = `import java.nio.file.*;

public final class BitsBoundProbe {
    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] data = Files.readAllBytes(Paths.get(path));
            bitsbound.TFixed.Value[] vals = new bitsbound.TFixed.Value[4];
            for (int i = 0; i < vals.length; i++) { vals[i] = new bitsbound.TFixed.Value(); }
            bitsbound.TableFixed.Entry[] plan = bitsbound.TableFixed.plan(1 << 10);
            short[] remap = new short[1 << 10];
            bitsbound.TableFixed.Report r = new bitsbound.TableFixed.Report();
            int n = bitsbound.TFixed.load(vals, vals.length, data, plan, remap, bitsbound.TFixed.image(), r);
            System.out.println(Paths.get(path).getFileName() + (r.refused ? " refused" : " read " + n));
        }
    }
}
`

// javaBitsBoundWire encodes one T with the Go writer.
func javaBitsBoundWire(t *testing.T, m *tabletext.Model, small, wide uint64) []byte {
	t.Helper()
	inst := m.New(m.Lookup("T"))
	s, _ := inst.FieldByKey("small")
	s.Cell.U = small
	w, _ := inst.FieldByKey("wide")
	w.Cell.U = wide
	wire, err := tablewire.EncodeFixed(m, inst)
	if err != nil {
		t.Fatalf("encode small=%d wide=%d: %v", small, wide, err)
	}
	return wire
}

// javaBitsBoundOffset is the storage offset of a field: the one byte where a
// record holding 1 in it differs from a record holding 0 (little-endian, so
// that byte is the storage's first).
func javaBitsBoundOffset(t *testing.T, zero, one []byte) int {
	t.Helper()
	if len(zero) != len(one) {
		t.Fatalf("records of %d and %d bytes", len(zero), len(one))
	}
	at := -1
	for i := range zero {
		if zero[i] != one[i] {
			if at >= 0 {
				t.Fatalf("the records differ at %d and %d, want one byte", at, i)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the records do not differ")
	}
	return at
}

func TestFixedBitsUnsignedBound(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, javaBitsBoundSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	m := tabletext.NewModel(u)
	zero := javaBitsBoundWire(t, m, 0, 0)
	smallAt := javaBitsBoundOffset(t, zero, javaBitsBoundWire(t, m, 1, 0))
	wideAt := javaBitsBoundOffset(t, zero, javaBitsBoundWire(t, m, 0, 1))
	clean := javaBitsBoundWire(t, m, 1<<5-1, 1<<40-1)
	// The clean record already holds 2^N - 1 in each field's storage.
	if got := binary.LittleEndian.Uint32(clean[smallAt:]); got != 1<<5-1 {
		t.Fatalf("small's storage holds %#x, want %#x", got, 1<<5-1)
	}
	if got := binary.LittleEndian.Uint64(clean[wideAt:]); got != 1<<40-1 {
		t.Fatalf("wide's storage holds %#x, want %#x", got, uint64(1<<40-1))
	}

	dir := t.TempDir()
	type row struct {
		name   string
		wire   []byte
		refuse bool
	}
	forge := func(name string, at, width int, v uint64) row {
		w := bytes.Clone(clean)
		if width == 4 {
			binary.LittleEndian.PutUint32(w[at:], uint32(v))
		} else {
			binary.LittleEndian.PutUint64(w[at:], v)
		}
		return row{name: name, wire: w, refuse: true}
	}
	rows := []row{
		{name: "max", wire: clean},
		forge("small_2pow5", smallAt, 4, 1<<5),
		forge("small_topbit", smallAt, 4, 0x80000011),
		forge("wide_2pow40", wideAt, 8, 1<<40),
		forge("wide_topbit", wideAt, 8, 0x8000000000000011),
	}
	var paths []string
	for _, r := range rows {
		p := filepath.Join(dir, r.name+".bin")
		if err := os.WriteFile(p, r.wire, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	writeFixedJava(t, src, "bitsbound", files)
	probe := filepath.Join(src, "BitsBoundProbe.java")
	if err := os.WriteFile(probe, []byte(javaBitsBoundProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "bitsbound", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := exec.Command(javac, append(args, probe)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, append([]string{"-ea", "-cp", classes, "BitsBoundProbe"}, paths...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		want := fmt.Sprintf("%s.bin read 1", r.name)
		if r.refuse {
			want = fmt.Sprintf("%s.bin refused", r.name)
		}
		if lines[i] != want {
			t.Errorf("%s: %s, want %s", r.name, lines[i], want)
		}
	}
}
