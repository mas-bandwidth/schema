package goose

// THE COMPRESSED FLOAT'S WIRE UNDER `goose build` (docs/SPEC.md §4.3,
// docs/PORT-GOOSE.md, FLOAT32). This is a functional test: it writes a Goose
// packet program, builds it with `goose build -O2` (the machine's C compiler,
// the path a production build takes, not `goose run`'s TinyCC), sweeps a
// compressed float over [0, 10] at resolution 0.01 in steps of 0.0005 — every
// quantization tie included — and compares the wire bytes and the decoded
// float32 bits the built program prints against the Go reference's writer and
// reader run in-process on the same values.

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const compressedFloatSchema = `package cfloat

type Packet
{
    value float32 | min = 0.0, max = 10.0, resolution = 0.01
}
`

const compressedFloatMain = `import os;
import .Packet;
import .serialize;

fn hex_append(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for c in b { out.push(digits[c >> 4]); out.push(digits[c & 15]); }
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("sweep.bin", data) { abort("read sweep.bin"); }
    var rbuf: u8[72] = [0; 72];
    var i: i64 = 0;
    while i + 4 <= data.len {
        let bits: u32 = (data[i] as u32) | ((data[i + 1] as u32) << 8)
            | ((data[i + 2] as u32) << 16) | ((data[i + 3] as u32) << 24);
        let v = serialize_f32_from_bits(bits);
        var p = cfloat::Packet { .. };
        p.Value = v;
        var wbuf: u8[64] = [0; 64];
        var w = serialize_write_stream_init(wbuf);
        if cfloat::WritePacket(w, p) != SERIALIZE_ERROR_NONE { abort("write refused"); }
        serialize_flush(w);
        let n = serialize_bytes_processed(w);
        var line: u8[>..] = [];
        hex_append(line, wbuf[0..n]);
        var r = serialize_read_stream_init_padded(rbuf, wbuf[0..n]);
        var out = cfloat::Packet { .. };
        if !cfloat::ReadPacket(r, out) { abort("read refused"); }
        let db = serialize_f32_bits(out.Value);
        let dbytes: u8[4] = [(db >> 24) as! u8, (db >> 16) as! u8, (db >> 8) as! u8, db as! u8];
        line.push(' ');
        hex_append(line, dbytes);
        print(line);
        i += 4;
    }
}
`

// compressedFloatUnit parses the schema in-process and answers its IR.
func compressedFloatUnit(t *testing.T) *ir.Unit {
	t.Helper()
	tree, perrs := parser.Parse("Packet.schema", []byte(compressedFloatSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Packet.schema", Name: "Packet.schema", Base: "Packet",
		Bytes: []byte(compressedFloatSchema), AST: tree,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// compressedFloatSweep is every value the comparison covers: [0, 10] in steps
// of 0.0005 as float32, every quantization tie included.
func compressedFloatSweep() []float32 {
	const n = 20000
	values := make([]float32, 0, n+1)
	for i := 0; i <= n; i++ {
		values = append(values, float32(float64(i)*0.0005))
	}
	return values
}

// refCompressedFloatWrite is the Go reference's writer arithmetic over [0, 10]
// at resolution 0.01, with the two float32 roundings the reference pins.
func refCompressedFloatWrite(v float32) uint32 {
	const steps = float32(1000)
	const delta = float32(10)
	const min = float32(0)
	normalized := (v - min) / delta
	if !(normalized >= 0) {
		normalized = 0
	} else if !(normalized <= 1) {
		normalized = 1
	}
	iv := uint32(float32(normalized*steps) + 0.5)
	if iv > uint32(steps) {
		iv = uint32(steps)
	}
	return iv
}

// refCompressedFloatRead is the Go reference's reader arithmetic: the
// dequantized float32 bits for a quantized integer.
func refCompressedFloatRead(iv uint32) uint32 {
	const steps = float32(1000)
	const delta = float32(10)
	const min = float32(0)
	normalized := float32(iv) / steps
	return math.Float32bits(float32(normalized*delta) + min)
}

// refCompressedFloatWire packs one quantized integer LSB-first in its wire
// width, as the Go reference's bit packer flushes it.
func refCompressedFloatWire(iv uint32, bits int) []byte {
	out := make([]byte, (bits+7)/8)
	for i := 0; i < bits; i++ {
		if iv>>uint(i)&1 != 0 {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out
}

func refCompressedFloatLine(iv uint32, bits int) string {
	return fmt.Sprintf("%x %08x", refCompressedFloatWire(iv, bits), refCompressedFloatRead(iv))
}

// gooseCommand is exec.Command for the processes this gate starts. The gate
// must run under a plain `go test` (its done-when sets no environment), so it
// carries its own wall-clock bound instead of the shared slow-test gate.
func gooseCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, name, args...)
}

// gooseBuildForTest writes files into dir and builds main with `goose build
// -O2`, answering the executable beside it. cc, when non-empty, is handed to
// the build as GOOSE_CC (the negative control's flag-forcing wrapper).
func gooseBuildForTest(t *testing.T, dir string, files map[string][]byte, main, cc string) string {
	t.Helper()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatal(why)
	}
	exe := strings.TrimSuffix(main, ".goose")
	build := gooseCommand(t, goose, "build", "-O2", main, "-o", exe)
	build.Dir = dir
	if cc != "" {
		build.Env = append(os.Environ(), "GOOSE_CC="+cc)
	}
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("goose build -O2 %s: %v\n%s", main, err, out)
	}
	return filepath.Join(dir, exe)
}

// TestGooseCompressedFloatBuildMatchesGo sweeps a compressed float through a
// program built by `goose build -O2` and requires every wire byte and decoded
// float32 bit to equal the Go reference's.
func TestGooseCompressedFloatBuildMatchesGo(t *testing.T) {
	t.Parallel()
	files, err := Generate(compressedFloatUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	files["main.goose"] = []byte(compressedFloatMain)

	values := compressedFloatSweep()
	var sweep bytes.Buffer
	for _, v := range values {
		bits := math.Float32bits(v)
		sweep.Write([]byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)})
	}
	files["sweep.bin"] = sweep.Bytes()

	dir := t.TempDir()
	exe := gooseBuildForTest(t, dir, files, "main.goose", "")

	run := gooseCommand(t, exe)
	run.Dir = dir
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(exe), err, stderr.Bytes())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != len(values) {
		t.Fatalf("the built program printed %d lines, want %d\n%s", len(lines), len(values), stdout.Bytes())
	}
	const bits = 10
	for i, v := range values {
		want := refCompressedFloatLine(refCompressedFloatWrite(v), bits)
		if lines[i] != want {
			t.Fatalf("value %v (bits %08x): built program printed %q, Go reference %q", v, math.Float32bits(v), lines[i], want)
		}
	}
}
