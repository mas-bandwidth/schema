// The packet wire's hostile-bytes fuzzer for its read-side text refusals
// (schema#558; SPEC §4.7, §4.12).
//
// `schema_test_random` (test/random_main.cpp) is a round-trip sweep over VALID
// values: it never feeds a reader bytes it did not write. The table wire has a
// mutant fuzzer over its corpus (test/conformance/harness/wirefuzz.go); the
// packet wire had none, so its text refusals — the interior-null scan and the
// UTF-8 well-formedness check landed for `string(N)` by #556 — were gated only
// by the corpus replay and the negative controls.
//
// This is that fuzzer, over the EMITTED C++ and C readers: each emitter's
// `string(N)` read validation is compiled straight from generated source and
// fed mutants of pinned seeds, and every accept/refuse verdict is compared
// against an INDEPENDENT oracle — Go's `unicode/utf8` plus its own
// interior-null scan, not the reader arithmetic under test. The C++ reference
// is the authority for bytes; the oracle here is a second implementation,
// which is the whole reason the comparison can disagree.
//
// The seeds are pinned BY NAME (`string-accept-astral-code-point`,
// `string-refuse-overlong-three-byte-slash`, ...) so a red names the vector
// that reproduces it, and every mutant is a pure function of (seed, index).
// The negative control plants a reader defect — the UTF-8 refusal removed from
// a copy of the compiled reader — and requires this fuzzer to go red on it.
package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// packetTextSeed is one pinned vector: a name from the #556/#188 corpus and
// the payload, hex. The names mirror serialize's shared `string.txt` row names
// so a red reads as the refusal it is.
type packetTextSeed struct {
	name string
	hex  string
}

// packetTextSeeds is the ACCEPT/REFUSE corpus, pinned by name. Both directions
// are present on purpose: a reader that refuses a whole byte class fails an
// accept vector, which is exactly the failure the seed pairs exist to catch.
var packetTextSeeds = []packetTextSeed{
	{"string-accept-empty", ""},
	{"string-accept-ascii", "68656c6c6f"},
	{"string-accept-shortest-two-byte-sequence", "c2a2"},
	{"string-accept-shortest-three-byte-sequence", "e0a080"},
	{"string-accept-just-below-the-surrogate-block", "ed9fbf"},
	{"string-accept-just-above-the-surrogate-block", "ee8080"},
	{"string-accept-astral-code-point", "f0908080"},
	{"string-accept-the-largest-code-point", "f48fbfbf"},
	{"string-refuse-overlong-two-byte-slash", "c0af"},
	{"string-refuse-overlong-three-byte-slash", "e080af"},
	{"string-refuse-surrogate-encoded-in-utf8", "eda080"},
	{"string-refuse-low-surrogate-encoded-in-utf8", "edb080"},
	{"string-refuse-code-point-above-10ffff", "f4908080"},
	{"string-refuse-five-byte-sequence", "f888808080"},
	{"string-refuse-lone-continuation-byte", "80"},
	{"string-refuse-continuation-byte-after-ascii", "6180"},
	{"string-refuse-truncated-two-byte-sequence", "c2"},
	{"string-refuse-truncated-three-byte-sequence", "e0a0"},
	{"string-refuse-byte-fe", "fe"},
	{"string-refuse-byte-ff", "ff"},
	{"string-refuse-interior-null", "610062"},
	{"string-refuse-leading-null", "0061"},
}

// packetTextOracle is the second implementation: standard UTF-8
// well-formedness (Unicode Table 3-7) plus the interior-null rule of §4.7.
// It shares no code with the emitted C++ under test.
func packetTextOracle(payload []byte) bool {
	return utf8.Valid(payload) && bytes.IndexByte(payload, 0) < 0
}

// packetTextMutants turns one seed into the mutants a reader must answer for.
// Every mutant is a pure function of (seed, index), so a red reproduces from
// the index alone. The operations are the ones hostile bytes arrive by: a bit
// flipped, a byte rewritten, a payload truncated, a byte inserted. The random
// pass never invents a new seed, so the sequence is a function of the pinned
// corpus.
func packetTextMutants(seed []byte, index int) []byte {
	state := uint64(index)*0x9E3779B97F4A7C15 + 0xD1B54A32D192ED03
	next := func() uint64 {
		state ^= state >> 12
		state ^= state << 25
		state ^= state >> 27
		return state * 0x2545F4914F6CDD1D
	}
	out := append([]byte(nil), seed...)
	op := next() % 4
	switch op {
	case 0: // flip one bit
		if len(out) == 0 {
			out = append(out, 0)
		}
		i := next() % uint64(len(out))
		out[i] ^= 1 << (next() % 8)
	case 1: // rewrite one byte
		if len(out) == 0 {
			out = append(out, 0)
		}
		i := next() % uint64(len(out))
		out[i] = byte(next())
	case 2: // truncate
		if len(out) > 0 {
			out = out[:next()%uint64(len(out)+1)]
		}
	case 3: // insert one byte
		i := next() % uint64(len(out)+1)
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = byte(next())
	}
	return out
}

// packetTextLeg is one emitter's read validators as this fuzzer drives them:
// the symbol names and the C/C++ spelling each backend emits.
type packetTextLeg struct {
	lang     string   // the compiler target
	compiler string   // the driver on PATH
	ext      string   // the driver's file name suffix
	utf8     string   // the UTF-8 validator's symbol
	null     string   // the interior-null scan's symbol
	types    []string // the preamble the extracted helpers need
}

var packetTextLegs = []packetTextLeg{
	{
		lang: "cpp", compiler: "c++", ext: ".cpp",
		utf8: "schema_utf8_valid", null: "schema_interior_null",
		types: []string{"#define SCHEMA_READ_INLINE inline\n", "#define SCHEMA_UNUSED\n"},
	},
	{
		lang: "c", compiler: "cc", ext: ".c",
		utf8: "schema_utf8_valid_", null: "schema_interior_null_",
		types: []string{
			"#define SCHEMA_READ_INLINE inline\n",
			"#define SCHEMA_C_READ_INLINE inline\n",
			"#define SCHEMA_UNUSED __attribute__( ( unused ) )\n",
			"typedef uint8_t serialize_uint8_t;\n",
			"typedef uint32_t serialize_uint32_t;\n",
			"typedef uint64_t serialize_uint64_t;\n",
		},
	},
}

// packetTextDriverSource is the compiled leg: the extracted read validators
// followed by a frame reader that prints one verdict per stdin payload. With
// defect the UTF-8 refusal is removed AFTER the real function is defined, so
// the reader compiled here is the reference reader with exactly one rule
// missing — the reader defect the control requires.
func packetTextDriverSource(leg packetTextLeg, helpers string, defect bool) string {
	var b strings.Builder
	b.WriteString("#include <stdint.h>\n")
	b.WriteString("#include <string.h>\n")
	b.WriteString("#include <stdio.h>\n")
	for _, line := range leg.types {
		b.WriteString(line)
	}
	b.WriteString(helpers)
	if defect {
		b.WriteString("// NEGATIVE CONTROL: the reader's UTF-8 refusal is gone.\n")
		fmt.Fprintf(&b, "#define %s( a, b ) 1\n", leg.utf8)
	}
	fmt.Fprintf(&b, `
int main( void )
{
    uint8_t buffer[ 4096 ];
    for ( ;; )
    {
        uint32_t length;
        if ( fread( &length, 4, 1, stdin ) != 1 )
            break;
        if ( length > sizeof( buffer ) )
            return 2;
        if ( length && fread( buffer, 1, length, stdin ) != length )
            return 3;
        const int accept = !%s( buffer, (int32_t) length )
            && %s( buffer, (int32_t) length );
        putchar( accept ? '1' : '0' );
    }
    return 0;
}
`, leg.null, leg.utf8)
	return b.String()
}

// extractCppFunction returns one brace-matched function from generated source.
func extractCppFunction(t *testing.T, source, name string) string {
	t.Helper()
	at := strings.Index(source, name+"(")
	if at < 0 {
		at = strings.Index(source, name+" (")
	}
	if at < 0 {
		t.Fatalf("generated source declares no %s", name)
	}
	begin := strings.LastIndex(source[:at], "\n") + 1
	open := at + strings.Index(source[at:], "{")
	depth := 1
	end := open + 1
	for ; end < len(source) && depth > 0; end++ {
		switch source[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	if depth != 0 {
		t.Fatalf("unclosed %s in generated source", name)
	}
	return source[begin:end] + "\n"
}

// compilePacketTextDriver compiles one leg's read validators into a driver and
// returns its path, skipping where that leg's compiler is not installed rather
// than failing a bare machine.
func compilePacketTextDriver(t *testing.T, dir string, leg packetTextLeg, defect bool) string {
	t.Helper()
	compiler, err := exec.LookPath(leg.compiler)
	if err != nil {
		t.Skipf("%s unavailable: %v", leg.compiler, err)
	}
	u := unitFromSource(t, "package p\n\ntype Text { label string(8) }\n")
	files, err := New().Generate(u, leg.lang, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, data := range files {
		if strings.Contains(string(data), leg.utf8) {
			source = string(data)
			break
		}
	}
	if source == "" {
		t.Fatalf("generated %s carries no %s", leg.lang, leg.utf8)
	}
	helpers := extractCppFunction(t, source, leg.null) +
		extractCppFunction(t, source, leg.utf8)
	path := filepath.Join(dir, "packet_text_driver"+leg.ext)
	if err := os.WriteFile(path, []byte(packetTextDriverSource(leg, helpers, defect)), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "packet_text_driver")
	args := []string{"-O1", "-Wall", "-Wextra", "-Werror", path, "-o", bin}
	if leg.ext == ".cpp" {
		args = append([]string{"-std=c++17"}, args...)
	} else {
		args = append([]string{"-std=c99"}, args...)
	}
	if out, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile %s packet text driver: %v\n%s", leg.lang, err, out)
	}
	return bin
}

// runPacketTextFuzz feeds the pinned seeds and their mutants to the compiled
// leg and returns the first divergence (with the seed it came from), or the
// count of cases when there is none.
func runPacketTextFuzz(t *testing.T, bin string) (divergence string, cases int) {
	t.Helper()
	var stdin bytes.Buffer
	var inputs [][]byte
	add := func(payload []byte) {
		inputs = append(inputs, payload)
		var frame [4]byte
		binary.LittleEndian.PutUint32(frame[:], uint32(len(payload)))
		stdin.Write(frame[:])
		stdin.Write(payload)
	}
	for _, seed := range packetTextSeeds {
		payload, err := hex.DecodeString(seed.hex)
		if err != nil {
			t.Fatalf("seed %s: %v", seed.name, err)
		}
		add(payload)
	}
	names := make([]string, 0, len(packetTextSeeds))
	for _, seed := range packetTextSeeds {
		payload, _ := hex.DecodeString(seed.hex)
		names = append(names, seed.name+"#0")
		// the random pass draws from the seed's own mutants; thirty per seed
		// keeps the corpus deterministic and the compile+run inside a test
		for i := 0; i < 30; i++ {
			add(packetTextMutants(payload, i))
			names = append(names, fmt.Sprintf("%s#%d", seed.name, i+1))
		}
	}

	cmd := exec.Command(bin)
	cmd.Stdin = &stdin
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("run packet text driver: %v\n%s", err, stdout.String())
	}
	verdicts := stdout.Bytes()
	if len(verdicts) != len(inputs) {
		t.Fatalf("driver answered %d verdicts for %d inputs", len(verdicts), len(inputs))
	}
	for i, payload := range inputs {
		want := packetTextOracle(payload)
		got := verdicts[i] == '1'
		if want != got {
			what := "refused a payload the oracle accepts"
			if got {
				what = "accepted a payload the oracle refuses"
			}
			return fmt.Sprintf("%s at %s (payload %x)", what, names[i], payload), i + 1
		}
	}
	return "", len(inputs)
}

// TestPacketTextHostileFuzz is the gate: each emitted packet text reader and
// the independent oracle agree on every pinned vector and every mutant, and
// neither ever panics or aborts (a crash fails the process run).
func TestPacketTextHostileFuzz(t *testing.T) {
	for _, leg := range packetTextLegs {
		t.Run(leg.lang, func(t *testing.T) {
			bin := compilePacketTextDriver(t, t.TempDir(), leg, false)
			if divergence, cases := runPacketTextFuzz(t, bin); divergence != "" {
				t.Fatalf("packet text fuzz: %s", divergence)
			} else {
				fmt.Printf("packet-text-fuzz[%s]: %d seeds, %d cases, 0 divergences\n", leg.lang, len(packetTextSeeds), cases)
			}
		})
	}
}

// TestPacketTextHostileFuzzNegativeControl plants a reader defect — the UTF-8
// refusal gone — and requires the SAME fuzzer to go red, naming the vector.
// A fuzzer that has never gone red proves nothing about the reader it watches.
func TestPacketTextHostileFuzzNegativeControl(t *testing.T) {
	for _, leg := range packetTextLegs {
		t.Run(leg.lang, func(t *testing.T) {
			bin := compilePacketTextDriver(t, t.TempDir(), leg, true)
			divergence, _ := runPacketTextFuzz(t, bin)
			if divergence == "" {
				t.Fatal("NEGATIVE CONTROL FAILED: the fuzzer stayed green with the UTF-8 refusal removed")
			}
			if !strings.Contains(divergence, "accepted a payload the oracle refuses") {
				t.Fatalf("NEGATIVE CONTROL FAILED: the leg went red, but not on the refusal: %s", divergence)
			}
			t.Logf("negative control red: %s", divergence)
		})
	}
}
