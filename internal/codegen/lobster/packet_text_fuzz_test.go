// The packet wire's hostile-bytes fuzzer, Lobster's leg (schema#558; SPEC
// §4.7). compiler/packettextfuzz_test.go is the fuzzer and its C++ and C legs:
// pinned seeds by name, mutants that are a pure function of (seed, index),
// and every accept/refuse verdict held to the Go oracle — unicode/utf8 plus
// the interior-null scan — never to the reader under test. This is the same
// fuzzer over the emitted Lobster READ: each payload rides a string(15) on
// the wire (its used length, then the bytes after the align, SPEC §4.7) and
// packettext.ReadNarrow answers. The seeds are read out of the C/C++ leg's
// own table (packetTextSeeds), so the legs fuzz one corpus and a seed added
// there is fuzzed here; the mutant function is that file's, copied below
// (a test package cannot import another's). The negative control removes the emitted
// UTF-8 refusal and requires the fuzzer to go red naming an accepted payload
// the oracle refuses.
package lobster

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

type packetFuzzSeed struct{ name, hex string }

// packetFuzzSeeds reads packetTextSeeds out of compiler/packettextfuzz_test.go.
func packetFuzzSeeds(t *testing.T) []packetFuzzSeed {
	t.Helper()
	path := filepath.Join("..", "..", "..", "compiler", "packettextfuzz_test.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var seeds []packetFuzzSeed
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "packetTextSeeds" || len(vs.Values) != 1 {
			return true
		}
		for _, e := range vs.Values[0].(*ast.CompositeLit).Elts {
			pair := e.(*ast.CompositeLit).Elts
			name, _ := strconv.Unquote(pair[0].(*ast.BasicLit).Value)
			h, _ := strconv.Unquote(pair[1].(*ast.BasicLit).Value)
			seeds = append(seeds, packetFuzzSeed{name, h})
		}
		return false
	})
	if len(seeds) < 20 {
		t.Fatalf("read %d seeds out of %s; the table moved", len(seeds), path)
	}
	return seeds
}

// packetFuzzMutant is compiler/packettextfuzz_test.go's packetTextMutants.
func packetFuzzMutant(seed []byte, index int) []byte {
	state := uint64(index)*0x9E3779B97F4A7C15 + 0xD1B54A32D192ED03
	next := func() uint64 {
		state ^= state >> 12
		state ^= state << 25
		state ^= state >> 27
		return state * 0x2545F4914F6CDD1D
	}
	out := append([]byte(nil), seed...)
	switch next() % 4 {
	case 0:
		if len(out) == 0 {
			out = append(out, 0)
		}
		i := next() % uint64(len(out))
		out[i] ^= 1 << (next() % 8)
	case 1:
		if len(out) == 0 {
			out = append(out, 0)
		}
		i := next() % uint64(len(out))
		out[i] = byte(next())
	case 2:
		if len(out) > 0 {
			out = out[:next()%uint64(len(out)+1)]
		}
	case 3:
		i := next() % uint64(len(out)+1)
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = byte(next())
	}
	return out
}

// packetFuzzOracle is the Go oracle, the C/C++ leg's packetTextOracle.
func packetFuzzOracle(payload []byte) bool {
	return utf8.Valid(payload) && bytes.IndexByte(payload, 0) < 0
}

const packetFuzzDriver = `import .Narrow
import .serialize

def unhex(s:string) -> string:
    if s == "-": return ""
    var b = ensure_size("", length(s) / 2, 0)
    for(length(s) / 2) i:
        let v, _ = string_to_int(substring(s, i * 2, 2), 16)
        b = write_int8_le(b, i, v)
    return b

// each payload rides as the writer lays a string(15) (SPEC §4.7): its used
// length in [0, 15], the align, then the bytes; the READ under test decides
var verdicts = ""
let text = read_file("payloads.txt")
assert text
for(tokenize(text, "\n", " ")) line:
    if length(line):
        let payload = unhex(line)
        let w = serialize_write_stream(ensure_size("", 64, 0))
        let _n, lok = w.serialize_int(length(payload), 0, 15)
        assert lok
        let _p, bok = w.serialize_bytes(payload, length(payload))
        assert bok
        w.flush()
        let r = serialize_read_stream_padded(substring(w.buffer, 0, w.bytes_processed()))
        let v = packettext.NewNarrow()
        verdicts += if packettext.ReadNarrow(r, v): "1" else: "0"
print(verdicts)
`

type packetFuzzCase struct {
	name    string
	payload []byte
}

// packetFuzzCases is every seed and mutantsPerSeed of its mutants, the C/C++
// leg's own pass at the count this leg is given.
func packetFuzzCases(t *testing.T, mutantsPerSeed int) []packetFuzzCase {
	t.Helper()
	var all []packetFuzzCase
	for _, s := range packetFuzzSeeds(t) {
		p, err := hex.DecodeString(s.hex)
		if err != nil {
			t.Fatalf("seed %s: %v", s.name, err)
		}
		all = append(all, packetFuzzCase{s.name + "#0", p})
		for i := range mutantsPerSeed {
			all = append(all, packetFuzzCase{fmt.Sprintf("%s#%d", s.name, i+1), packetFuzzMutant(p, i)})
		}
	}
	return all
}

// runPacketFuzz answers every case through the Lobster read and returns the
// first divergence from the Go oracle, or "".
func runPacketFuzz(t *testing.T, files map[string][]byte, all []packetFuzzCase) string {
	t.Helper()
	var in strings.Builder
	for _, c := range all {
		if len(c.payload) > 15 {
			t.Fatalf("%s is %d bytes, past the string(15) it rides", c.name, len(c.payload))
		}
		if len(c.payload) == 0 {
			in.WriteString("-\n")
		} else {
			in.WriteString(hex.EncodeToString(c.payload) + "\n")
		}
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", packetFuzzDriver)
	got := strings.TrimRight(string(lobsterrun.Run(t, dir, "main.lobster", map[string][]byte{"payloads.txt": []byte(in.String())})), "\n")
	if len(got) != len(all) {
		t.Fatalf("the driver answered %d verdicts for %d payloads: %q", len(got), len(all), got[:min(len(got), 80)])
	}
	for i, c := range all {
		want := packetFuzzOracle(c.payload)
		if (got[i] == '1') != want {
			what := "refused a payload the oracle accepts"
			if got[i] == '1' {
				what = "accepted a payload the oracle refuses"
			}
			return fmt.Sprintf("%s at %s (payload %x)", what, c.name, c.payload)
		}
	}
	return ""
}

// packetFuzzMutants is the per-seed count: the C/C++ leg's thirty.
const packetFuzzMutants = 30

// TestLobsterPacketTextHostileFuzz is packet-text-fuzz[lobster]: the emitted
// Lobster read and the Go oracle agree on every pinned seed and mutant.
func TestLobsterPacketTextHostileFuzz(t *testing.T) {
	t.Parallel()
	all := packetFuzzCases(t, packetFuzzMutants)
	if d := runPacketFuzz(t, generateText(t), all); d != "" {
		t.Fatalf("packet text fuzz: %s", d)
	}
	t.Logf("packet-text-fuzz[lobster]: %d cases, 0 divergences", len(all))
}

// TestLobsterPacketTextHostileFuzzNegativeControl removes the emitted UTF-8
// refusal and requires the same fuzzer to go red on an accepted payload the
// oracle refuses.
func TestLobsterPacketTextHostileFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "    if not string_utf8_valid(s, n):\n        return serialize_refusal_invalid_utf8\n"
	files := generateText(t)
	text := string(files["schema_wire.lobster"])
	if strings.Count(text, anchor) != 1 {
		t.Fatal("the UTF-8 refusal anchor is not in schema_wire.lobster once")
	}
	files["schema_wire.lobster"] = []byte(strings.Replace(text, anchor, "", 1))
	d := runPacketFuzz(t, files, packetFuzzCases(t, packetFuzzMutants))
	if !strings.HasPrefix(d, "accepted a payload the oracle refuses") {
		t.Fatalf("NEGATIVE CONTROL FAILED: with the UTF-8 refusal removed the fuzzer said %q", d)
	}
	t.Logf("negative control red: %s", d)
}
