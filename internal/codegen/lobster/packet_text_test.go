// Packet text on the Lobster type wire: string(N)'s UTF-8 rules (SPEC §4.7)
// and wstring(N)'s UTF-16 rules (SPEC §4.12), the Lobster rows of the
// packet-utf8 and packet-wide gates (make/checks/packet-text.mk,
// test/packet-text/harness). The vectors are the harness's: the shared
// corpus rows (testdata/conformance/text/string.txt at bound 16 for
// test/packet-text/Narrow.schema; wstring.txt at bounds 8 and 5 for
// examples-wide/WideText.schema's WideSeven and WideFour), SPEC §4.12's six
// interop cases, and every single-bit flip of every accepted row. The
// verdict on each is the one test/packet-text/driver.c prints ("REFUSE", or
// "OK <consumed> <payload> <written> <canonical bytes>"); the judge is the
// pinned corpus for its rows and, for the flips, an oracle written here from
// the SPEC rules, held to the corpus first exactly as the harness holds the
// C++ oracle. No C++ toolchain is needed, so the gate runs in the lobster
// unit leg. Two controls remove the Lobster reader's UTF-8 check and its
// surrogate pairing; each must turn the bit-flip agreement red.
package lobster

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

type textVector struct {
	name, wire, payload string
	bits, bound         int
	refused, mutation   bool
	wide                bool
}

func packedHex(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	if s == "" {
		return "-"
	}
	return s
}

// textCorpus reads the shared corpus as test/packet-text/harness does.
func textCorpus(t *testing.T, file string, wide bool) []textVector {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "conformance", "text", file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var all []textVector
	v := textVector{wide: wide}
	flush := func() {
		if v.name != "" && ((!wide && v.bound == 16) || (wide && (v.bound == 8 || v.bound == 5))) {
			all = append(all, v)
		}
		v = textVector{wide: wide}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, _ := strings.Cut(line, " ")
		switch key {
		case "name":
			v.name = rest
		case "param":
			_, value, _ := strings.Cut(rest, "=")
			v.bound, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				t.Fatal(err)
			}
		case "bytes":
			v.wire = packedHex(rest)
		case "expect":
			v.refused = rest == "refused"
			if !v.refused {
				_, value, _ := strings.Cut(rest, "=")
				v.payload = packedHex(value)
			}
		case "consumed":
			v.bits, err = strconv.Atoi(rest)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	flush()
	if len(all) < 10 {
		t.Fatalf("%s: only %d rows", file, len(all))
	}
	return all
}

// bitAt is wire bit i, LSB first.
func bitAt(b []byte, i int) uint64 {
	if i/8 >= len(b) {
		return 0
	}
	return uint64(b[i/8]>>(i%8)) & 1
}

func bitsAt(b []byte, at, n int) uint64 {
	var v uint64
	for k := range n {
		v |= bitAt(b, at+k) << k
	}
	return v
}

// narrowOracle is SPEC §4.7 for string(15): a 4-bit length in [0, 15], zero
// padding to the byte, the payload; no NUL, well-formed UTF-8.
func narrowOracle(wire []byte) string {
	total := len(wire) * 8
	if total < 4 {
		return "REFUSE"
	}
	n := int(bitsAt(wire, 0, 4))
	if total < 8+8*n || bitsAt(wire, 4, 4) != 0 {
		return "REFUSE"
	}
	payload := wire[1 : 1+n]
	if slices.Contains(payload, 0) {
		return "REFUSE"
	}
	if !utf8.Valid(payload) {
		return "REFUSE"
	}
	enc := append([]byte{byte(n)}, payload...)
	p := "-"
	if n > 0 {
		p = hex.EncodeToString(payload)
	}
	return fmt.Sprintf("OK %d %s %d %s", 8+8*n, p, 8+8*n, hex.EncodeToString(enc))
}

// wideOracle is SPEC §4.12 for wstring(bound): a 3-bit length in
// [0, bound], then one 32-bit group per code unit, no align; a group is
// nonzero and at most 0xFFFF, and surrogates pair.
func wideOracle(bound int, wire []byte) string {
	total := len(wire) * 8
	if total < 3 {
		return "REFUSE"
	}
	n := int(bitsAt(wire, 0, 3))
	if n > bound || total < 3+32*n {
		return "REFUSE"
	}
	var units []uint64
	low := false
	for i := range n {
		u := bitsAt(wire, 3+32*i, 32)
		isLow := u >= 0xDC00 && u <= 0xDFFF
		if u == 0 || u > 0xFFFF || isLow != low {
			return "REFUSE"
		}
		low = u >= 0xD800 && u <= 0xDBFF
		units = append(units, u)
	}
	if low {
		return "REFUSE"
	}
	bits := 3 + 32*n
	enc := make([]byte, (bits+7)/8)
	set := func(at int, v uint64, w int) {
		for k := range w {
			if v>>k&1 != 0 {
				enc[(at+k)/8] |= 1 << ((at + k) % 8)
			}
		}
	}
	set(0, uint64(n), 3)
	var p strings.Builder
	for i, u := range units {
		set(3+32*i, u, 32)
		fmt.Fprintf(&p, "%04x", u)
	}
	ps := p.String()
	if ps == "" {
		ps = "-"
	}
	es := hex.EncodeToString(enc)
	if es == "" {
		es = "-"
	}
	return fmt.Sprintf("OK %d %s %d %s", bits, ps, bits, es)
}

func textVectors(t *testing.T) []textVector {
	t.Helper()
	base := append(textCorpus(t, "string.txt", false), textCorpus(t, "wstring.txt", true)...)
	for i, units := range [][]uint16{{}, {0x043c, 0x0438, 0x0440}, {0xe000}, {0xffff}, {0x41, 0xd83d, 0xde00, 0x42}, {0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67}} {
		bits := 3 + 32*len(units)
		wire := make([]byte, (bits+7)/8)
		wire[0] = byte(len(units))
		var payload strings.Builder
		for j, unit := range units {
			fmt.Fprintf(&payload, "%04x", unit)
			for k := range 16 {
				if unit&(1<<k) != 0 {
					offset := 3 + 32*j + k
					wire[offset/8] |= 1 << (offset % 8)
				}
			}
		}
		base = append(base, textVector{name: fmt.Sprintf("wstring-interop-%d", i), wire: hex.EncodeToString(wire), payload: packedHex(payload.String()), bits: bits, bound: 8, wide: true})
	}
	all := append([]textVector{}, base...)
	for _, v := range base {
		if v.refused {
			continue
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(v.wire, "-"))
		if err != nil {
			t.Fatal(err)
		}
		for bit := 0; bit < len(raw)*8; bit++ {
			m := append([]byte{}, raw...)
			m[bit/8] ^= 1 << (bit % 8)
			all = append(all, textVector{name: fmt.Sprintf("%s/flip-%d", v.name, bit), wire: hex.EncodeToString(m), bound: v.bound, wide: v.wide, mutation: true})
		}
	}
	return all
}

// oracleVerdicts holds the oracle to the pinned corpus rows, then answers
// every vector.
func oracleVerdicts(t *testing.T, all []textVector) []string {
	t.Helper()
	out := make([]string, len(all))
	for i, v := range all {
		raw, _ := hex.DecodeString(strings.TrimPrefix(v.wire, "-"))
		if v.wide {
			out[i] = wideOracle(v.bound-1, raw)
		} else {
			out[i] = narrowOracle(raw)
		}
		if v.mutation {
			continue
		}
		want := "REFUSE"
		if !v.refused {
			want = fmt.Sprintf("OK %d %s %d %s", v.bits, v.payload, v.bits, v.wire)
		}
		if out[i] != want {
			t.Fatalf("the test's oracle disagrees with the pinned corpus on %s: %q, want %q", v.name, out[i], want)
		}
	}
	return out
}

func generateText(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, src := range []string{
		filepath.Join("..", "..", "..", "test", "packet-text", "Narrow.schema"),
		filepath.Join("..", "..", "..", "examples-wide", "WideText.schema"),
	} {
		dir := t.TempDir()
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, filepath.Base(src), string(data))
		out, err := Generate(unitFromDir(t, dir))
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, out)
	}
	return files
}

// lobsterTextVerdicts runs the Lobster driver over every vector, one line in
// and one verdict out, as test/packet-text/driver.c.
func lobsterTextVerdicts(t *testing.T, files map[string][]byte, all []textVector) []string {
	t.Helper()
	var in strings.Builder
	for _, v := range all {
		b := 0
		if v.wide {
			b = v.bound - 1
		}
		fmt.Fprintf(&in, "%d %s\n", b, v.wire)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", packetTextDriver)
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, dir, "main.lobster", map[string][]byte{"vectors.txt": []byte(in.String())})), "\n"), "\n")
	if len(out) != len(all) {
		t.Fatalf("the driver answered %d rows for %d vectors: %v", len(out), len(all), out[:min(len(out), 5)])
	}
	return out
}

// disagreements is the vectors whose Lobster verdict is not the oracle's.
func disagreements(all []textVector, got, want []string) []string {
	var bad []string
	for i := range all {
		if got[i] != want[i] {
			bad = append(bad, fmt.Sprintf("%s: lobster %q, oracle %q", all[i].name, got[i], want[i]))
		}
	}
	return bad
}

func TestLobsterPacketText(t *testing.T) {
	t.Parallel()
	all := textVectors(t)
	want := oracleVerdicts(t, all)
	got := lobsterTextVerdicts(t, generateText(t), all)
	if bad := disagreements(all, got, want); len(bad) > 0 {
		t.Fatalf("%d of %d packet-text verdicts disagree, first: %v", len(bad), len(all), bad[:min(len(bad), 5)])
	}
	t.Logf("packet text: %d vectors agree", len(all))
}

// TestLobsterPacketTextNegativeControls remove one reader rule each, in the
// emitted read, and require the bit-flip agreement to fail on that rule's
// side only: UTF-8 (narrow rows) and surrogate pairing (wide rows). The
// pinned rows of that rule disagree too; the bit flips are the harness's
// mutation agreement and must go red.
func TestLobsterPacketTextNegativeControls(t *testing.T) {
	t.Parallel()
	all := textVectors(t)
	want := oracleVerdicts(t, all)
	for _, tc := range []struct {
		name, file, anchor, with string
		wide                     bool
	}{
		{"utf8", "schema_wire.lobster", "    if not string_utf8_valid(s, n):\n        return serialize_refusal_invalid_utf8\n", "", false},
		{"pairing", "WideText.lobster", "if (runit >= 0xDC00 and runit <= 0xDFFF) != rlow: return stream.fail(", "if false: return stream.fail(", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := generateText(t)
			text := string(files[tc.file])
			if !strings.Contains(text, tc.anchor) {
				t.Fatalf("the %s anchor is absent from %s", tc.name, tc.file)
			}
			files[tc.file] = []byte(strings.ReplaceAll(text, tc.anchor, tc.with))
			got := lobsterTextVerdicts(t, files, all)
			var red, other int
			for i, v := range all {
				if got[i] == want[i] {
					continue
				}
				switch {
				case v.wide != tc.wide:
					other++
				case v.mutation:
					red++
				}
			}
			if red == 0 || other != 0 {
				t.Fatalf("removing %s: %d bit flips on its side disagree (want > 0), %d vectors of the other kind (want 0)", tc.name, red, other)
			}
		})
	}
}

const packetTextDriver = `import .Narrow
import .WideText
import .serialize

def unhex(s:string) -> string:
    if s == "-": return ""
    var b = ensure_size("", length(s) / 2, 0)
    for(length(s) / 2) i:
        let v, _ = string_to_int(substring(s, i * 2, 2), 16)
        b = write_int8_le(b, i, v)
    return b

def hexs(b:string) -> string:
    if length(b) == 0: return "-"
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

def units(b:string, n:int) -> string:
    if n == 0: return "-"
    var s = ""
    for(n) i:
        s += number_to_string(read_uint16_le(b, i * 2), 16, 4)
    return lowercase(s)

def written(write) -> int, string:
    let w = serialize_write_stream(ensure_size("", 256, 0))
    write(w)
    let bits = w.bits_processed()
    w.flush()
    return bits, substring(w.buffer, 0, w.bytes_processed())

let text = read_file("vectors.txt")
if text:
    for(tokenize(text, "\n", " ")) line:
        if length(line):
            let parts = tokenize(line, " ", "")
            let bound, _ = string_to_int(parts[0])
            let r = serialize_read_stream_padded(unhex(parts[1]))
            if bound == 0:
                let v = packettext.NewNarrow()
                if not packettext.ReadNarrow(r, v):
                    print("REFUSE")
                else:
                    let bits, enc = written() w: packettext.WriteNarrow(w, v)
                    print("OK " + string(r.bits_processed()) + " " + hexs(substring(v.Text, 0, v.TextLength)) + " " + string(bits) + " " + hexs(enc))
            elif bound == 7:
                let v = wide.NewWideSeven()
                if not wide.ReadWideSeven(r, v):
                    print("REFUSE")
                else:
                    let bits, enc = written() w: wide.WriteWideSeven(w, v)
                    print("OK " + string(r.bits_processed()) + " " + units(v.Text, v.TextLength) + " " + string(bits) + " " + hexs(enc))
            else:
                let v = wide.NewWideFour()
                if not wide.ReadWideFour(r, v):
                    print("REFUSE")
                else:
                    let bits, enc = written() w: wide.WriteWideFour(w, v)
                    print("OK " + string(r.bits_processed()) + " " + units(v.Text, v.TextLength) + " " + string(bits) + " " + hexs(enc))
`
