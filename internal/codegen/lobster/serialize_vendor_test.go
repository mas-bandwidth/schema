package lobster

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterVendoredSerializeIsThePin holds the vendored serialize.lobster
// port to the pin SERIALIZE_PIN names, then runs it: docs/PORT-LOBSTER.md,
// THE RUNTIME LAYOUT, says runtime/*.lobster is go:embed by glob and emitted
// beside the generated modules, and the type-wire cards call this library
// rather than a private bit reader or writer. The two byte records below are
// `writer canonical` records pasted from the port's vendored corpus
// (conformance/int.txt and conformance/float.txt); the test reads no .ref/
// checkout and adds no serialize dependency, because none exists in this
// module.
func TestLobsterVendoredSerializeIsThePin(t *testing.T) {
	t.Parallel()

	repo, commit, pinned := readLobsterSerializePin(t)
	if !strings.Contains(repo, "serialize.lobster") {
		t.Fatalf("SERIALIZE_PIN repository %q is not the serialize.lobster port", repo)
	}
	if len(commit) != 40 {
		t.Fatalf("SERIALIZE_PIN commit %q is not a full 40-hex commit sha", commit)
	}

	for name, want := range pinned {
		if matched, _ := filepath.Match("serialize*.lobster", name); !matched {
			t.Errorf("SERIALIZE_PIN lists %q, which is not a vendored serialize*.lobster file", name)
			continue
		}
		data, err := os.ReadFile(filepath.Join("runtime", name))
		if err != nil {
			t.Errorf("runtime/%s is listed in SERIALIZE_PIN but absent: %v", name, err)
			continue
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("runtime/%s sha256 is %s, SERIALIZE_PIN says %s", name, got, want)
		}
	}

	entries, err := os.ReadDir("runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if matched, _ := filepath.Match("serialize*.lobster", name); !matched {
			continue
		}
		if _, ok := pinned[name]; !ok {
			t.Errorf("runtime/%s is present but SERIALIZE_PIN does not list it", name)
		}
	}

	unit := &ir.Unit{
		Package:    "hello",
		ProtocolId: 0x11,
		Files:      []*ir.File{{Base: "Hello"}},
		DeclFile:   map[string]string{},
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	for name := range pinned {
		if _, ok := files[name]; !ok {
			t.Errorf("Generate omitted the vendored runtime %s", name)
		}
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", `import .serialize
import .Hello

`+lobsterrun.HexProbe+`
def write_int(value:int, min:int, max:int) -> string:
    let w = serialize_write_stream(ensure_size("", 8, 0))
    var v = value
    var ok = true
    v, ok = w.serialize_int(v, min, max)
    assert ok
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed())

def read_int(data:string, min:int, max:int) -> int:
    let r = serialize_read_stream_padded(data)
    var v = 0
    var ok = true
    v, ok = r.serialize_int(v, min, max)
    assert ok
    return v

def write_float(value:float) -> string:
    let w = serialize_write_stream(ensure_size("", 8, 0))
    var v = value
    var ok = true
    v, ok = w.serialize_float(v)
    assert ok
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed())

def read_float(data:string) -> float:
    let r = serialize_read_stream_padded(data)
    var v = 0.0
    var ok = true
    v, ok = r.serialize_float(v)
    assert ok
    return v

assert hello.ProtocolId == 17

let i = write_int(5, 0, 7)
hex(i)
hex(write_int(read_int(i, 0, 7), 0, 7))

let f = write_float(3.141592653589793)
hex(f)
hex(write_float(read_float(f)))
`)
	out := lobsterrun.Run(t, dir, "main.lobster", nil)
	// The vendored port compiles with one unused-variable warning, which the
	// pinned Lobster prints to stdout before the program runs. It is the
	// vendored file's text and is not edited here (THE TASK: verbatim), so the
	// warning lines are dropped and only the program's hex lines are read.
	var hexOut strings.Builder
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "warning:") {
			continue
		}
		hexOut.WriteString(line)
		hexOut.WriteByte('\n')
	}
	lines := lobsterrun.HexLines(t, []byte(hexOut.String()))
	if len(lines) != 4 {
		t.Fatalf("main.lobster printed %d hex lines, want 4", len(lines))
	}

	// conformance/int.txt, int-range-0-7-costs-three-bits
	intRecord := []byte{0x05}
	// conformance/float.txt, float-pi
	floatRecord := []byte{0xdb, 0x0f, 0x49, 0x40}
	for _, tc := range []struct {
		what string
		got  []byte
		want []byte
	}{
		{"serialize_int write", lines[0], intRecord},
		{"serialize_int read then write", lines[1], intRecord},
		{"serialize_float write", lines[2], floatRecord},
		{"serialize_float read then write", lines[3], floatRecord},
	} {
		if !bytes.Equal(tc.got, tc.want) {
			t.Errorf("%s is %x, the writer canonical record is %x", tc.what, tc.got, tc.want)
		}
	}
}

func readLobsterSerializePin(t *testing.T) (repo, commit string, files map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("runtime", "SERIALIZE_PIN"))
	if err != nil {
		t.Fatalf("runtime/SERIALIZE_PIN: %v", err)
	}
	files = map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "repository":
			if len(fields) != 2 {
				t.Fatalf("SERIALIZE_PIN line %d: repository takes one value", i+1)
			}
			repo = fields[1]
		case "commit":
			if len(fields) != 2 {
				t.Fatalf("SERIALIZE_PIN line %d: commit takes one value", i+1)
			}
			commit = fields[1]
		case "sha256":
			if len(fields) != 3 {
				t.Fatalf("SERIALIZE_PIN line %d: sha256 takes a hash and a file", i+1)
			}
			files[fields[2]] = fields[1]
		default:
			t.Fatalf("SERIALIZE_PIN line %d: unknown field %q", i+1, fields[0])
		}
	}
	if len(files) == 0 {
		t.Fatal("SERIALIZE_PIN lists no files")
	}
	return repo, commit, files
}
