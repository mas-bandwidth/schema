package goose

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// serializePin is runtime/SERIALIZE_PIN (docs/PORT-GOOSE.md, THE RUNTIME
// LAYOUT): the serialize.goose repository and commit the vendored files were
// copied from, and one sha256 per file.
type serializePin struct {
	repository string
	commit     string
	files      map[string]string
}

func loadSerializePin(t *testing.T) serializePin {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("runtime", "SERIALIZE_PIN"))
	if err != nil {
		t.Fatalf("read runtime/SERIALIZE_PIN: %v", err)
	}
	pin := serializePin{files: map[string]string{}}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "repository "):
			pin.repository = strings.TrimSpace(strings.TrimPrefix(line, "repository "))
		case strings.HasPrefix(line, "commit "):
			pin.commit = strings.TrimSpace(strings.TrimPrefix(line, "commit "))
		case strings.HasPrefix(line, "sha256 "):
			f := strings.Fields(line)
			if len(f) != 3 {
				t.Fatalf("runtime/SERIALIZE_PIN line %d: want `sha256  <hash>  <file>`, got %q", n+1, line)
			}
			pin.files[f[2]] = f[1]
		default:
			t.Fatalf("runtime/SERIALIZE_PIN line %d: unknown directive %q", n+1, line)
		}
	}
	if pin.repository == "" {
		t.Fatal("runtime/SERIALIZE_PIN names no repository")
	}
	if len(pin.commit) != 40 {
		t.Fatalf("runtime/SERIALIZE_PIN commit %q is not a full 40-character commit", pin.commit)
	}
	if len(pin.files) == 0 {
		t.Fatal("runtime/SERIALIZE_PIN lists no files")
	}
	return pin
}

// TestGooseVendoredSerializeIsThePin checks the vendored serialize.goose
// files against runtime/SERIALIZE_PIN (docs/PORT-GOOSE.md, THE RUNTIME
// LAYOUT): every file the pin lists is present with its sha256, and no
// serialize*.goose is present that the pin does not list. It then runs the
// vendored entry beside a generated unit under the pinned compiler and
// round-trips one int and one float through the write and read streams,
// comparing the written bytes with two canonical records of serialize.goose's
// corpus (conformance/int.txt, conformance/float.txt) pasted below.
func TestGooseVendoredSerializeIsThePin(t *testing.T) {
	t.Parallel()
	pin := loadSerializePin(t)

	for name, want := range pin.files {
		data, err := os.ReadFile(filepath.Join("runtime", name))
		if err != nil {
			t.Errorf("runtime/SERIALIZE_PIN lists %s, which is not vendored: %v", name, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("runtime/%s sha256 is %s, SERIALIZE_PIN says %s", name, got, want)
		}
	}

	vendored, err := filepath.Glob(filepath.Join("runtime", "serialize*.goose"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(vendored)
	for _, path := range vendored {
		name := filepath.Base(path)
		if _, ok := pin.files[name]; !ok {
			t.Errorf("runtime/%s is vendored but SERIALIZE_PIN does not list it", name)
		}
	}

	u := &ir.Unit{
		Package:    "wire",
		ProtocolId: 0x22,
		Files:      []*ir.File{{Base: "Wire"}},
		DeclFile:   map[string]string{},
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeAll(t, dir, Runtime())
	writeFile(t, dir, "main.goose", `import .Wire;
import .serialize;

fn hex(b: const u8[:]) {
    var s: u8[>..] = [];
    let digits = "0123456789abcdef";
    for c in b { s.push(digits[c >> 4]); s.push(digits[c & 15]); }
    print(s);
}

fn main() {
    // serialize.goose conformance/int.txt, writer canonical:
    // int-range-0-8-costs-four-bits (min 0, max 8, value 8, bytes 08).
    var wbuf: u8[8] = [0, 0, 0, 0, 0, 0, 0, 0];
    var w = serialize_write_stream_init(wbuf);
    assert(serialize_int(w, 8, 0, 8));
    serialize_flush(w);
    let wn = serialize_bytes_processed(w);
    hex(wbuf[0..wn]);

    var rbuf: u8[16] = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
    var i: i64 = 0;
    while i < wn { rbuf[i] = wbuf[i]; i += 1; }
    var r = serialize_read_stream_init(rbuf, wn);
    var iv: i32 = 0;
    assert(serialize_int(r, iv, 0, 8));
    print(iv);

    // serialize.goose conformance/float.txt, writer canonical:
    // float-pi (bytes DB 0F 49 40, bits 0x40490FDB).
    var fbuf: u8[8] = [0, 0, 0, 0, 0, 0, 0, 0];
    var fw = serialize_write_stream_init(fbuf);
    let pi = serialize_f32_from_bits(0x40490FDB);
    assert(serialize_float(fw, pi));
    serialize_flush(fw);
    let fbytes = serialize_bytes_processed(fw);
    hex(fbuf[0..fbytes]);

    var frbuf: u8[16] = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
    i = 0;
    while i < fbytes { frbuf[i] = fbuf[i]; i += 1; }
    var fr = serialize_read_stream_init(frbuf, fbytes);
    var fv: f32 = 0.0;
    assert(serialize_float(fr, fv));
    print(serialize_f32_bits(fv));
}
`)
	out := string(gooserun.Run(t, dir, "main.goose", nil))

	// int-range-0-8-costs-four-bits and float-pi, the two writer canonical
	// records above, pasted as literals.
	want := []string{
		"08",
		"8",
		"db0f4940",
		"1078530011",
	}
	got := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("main.goose printed %d lines %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("main.goose line %d is %q, want %q", i+1, got[i], want[i])
		}
	}
}
