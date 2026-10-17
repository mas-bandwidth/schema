package gotable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// strictJSON checks that text is EXACTLY what the text writers must emit: a
// strict RFC 8259 text (encoding/json.Valid: no comment, no trailing comma, no
// NaN or Infinity, no leading zero), valid UTF-8, an escape set limited to the
// seven short escapes plus a lower-case `\u00xx` for a byte under 0x20 (so no
// `\u` escape can name a surrogate and there is no `\/`), ending with exactly
// one newline (§16.1). The error names the first offence and its byte offset.
func strictJSON(text []byte) error {
	// valid UTF-8 first, so a raw bad byte is the offence that is named
	if !utf8.Valid(text) {
		for i := 0; i < len(text); {
			if text[i] < utf8.RuneSelf {
				i++
				continue
			}
			r, size := utf8.DecodeRune(text[i:])
			if r == utf8.RuneError && size == 1 {
				return fmt.Errorf("invalid UTF-8 at offset %d", i)
			}
			i += size
		}
	}
	// exactly one trailing newline (docs/SPEC-TABLES.md §16.1)
	body := text
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return fmt.Errorf("text does not end with exactly one newline (offset %d)", len(body))
	}
	body = body[:len(body)-1]
	if len(body) > 0 && body[len(body)-1] == '\n' {
		return fmt.Errorf("text ends with more than one newline (offset %d)", len(body)-1)
	}
	// the escape set
	if err := scanEscapes(body); err != nil {
		return err
	}
	// the grammar
	if !json.Valid(body) {
		if err := nameGrammarOffence(body); err != nil {
			return err
		}
		return fmt.Errorf("malformed JSON text")
	}
	return nil
}

func lowerHex(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	}
	return 0, false
}

// scanEscapes walks the text, string by string, and holds every escape to the
// writer's set: `\"` `\\` `\b` `\f` `\n` `\r` `\t`, or `\u00xx` with
// lower-case hex for a byte under 0x20. Anything else — a `\/`, a `\u` naming a
// surrogate, a `\u` for a byte at or past 0x20, an upper-case hex digit — is an
// offence.
func scanEscapes(body []byte) error {
	for i := 0; i < len(body); {
		if body[i] != '"' {
			i++
			continue
		}
		i++
		inString := true
		for inString && i < len(body) {
			switch body[i] {
			case '"':
				inString = false
				i++
			case '\\':
				if i+1 >= len(body) {
					return fmt.Errorf("unterminated escape at offset %d", i)
				}
				e := body[i+1]
				switch e {
				case '"', '\\', 'b', 'f', 'n', 'r', 't':
					i += 2
				case '/':
					return fmt.Errorf("escaped forward slash not allowed at offset %d", i)
				case 'u':
					if i+6 >= len(body) {
						return fmt.Errorf("unterminated \\u escape at offset %d", i)
					}
					var val int
					for k := 0; k < 4; k++ {
						v, ok := lowerHex(body[i+2+k])
						if !ok {
							return fmt.Errorf("\\u escape with a non-lowercase-hex digit at offset %d", i)
						}
						val = val<<4 | v
					}
					if val >= 0xd800 && val <= 0xdfff {
						return fmt.Errorf("surrogate \\u escape at offset %d", i)
					}
					if body[i+2] != '0' || body[i+3] != '0' {
						return fmt.Errorf("non-minimal \\u escape at offset %d", i)
					}
					if val >= 0x20 {
						return fmt.Errorf("\\u escape names a byte >= 0x20 at offset %d", i)
					}
					i += 6
				default:
					return fmt.Errorf("unknown escape %q at offset %d", e, i)
				}
			default:
				i++
			}
		}
	}
	return nil
}

var (
	reTrailingComma = regexp.MustCompile(`,\s*[}\]]`)
	reComment        = regexp.MustCompile(`//|/\*`)
	reNaN            = regexp.MustCompile(`NaN`)
	reInfinity       = regexp.MustCompile(`Infinity`)
	reLeadZero       = regexp.MustCompile(`(^|[:\[])\s*-?0[0-9]`)
)

// nameGrammarOffence answers the first RFC 8259 grammar offence it can name, so
// the negative controls each get their named offence instead of a generic
// "malformed" message. It runs only after json.Valid has already refused.
func nameGrammarOffence(body []byte) error {
	switch {
	case reTrailingComma.Match(body):
		return fmt.Errorf("trailing comma is not allowed")
	case reComment.Match(body):
		return fmt.Errorf("comment is not allowed")
	case reNaN.Match(body):
		return fmt.Errorf("NaN is not allowed")
	case reInfinity.Match(body):
		return fmt.Errorf("Infinity is not allowed")
	case reLeadZero.Match(body):
		return fmt.Errorf("leading zero is not allowed")
	}
	return nil
}

// setEnv sets an environment variable for the duration of the test. The tests
// are parallel (each opens with t.Parallel), so t.Setenv is forbidden; this
// helper restores the prior value on cleanup instead, and every key a test
// writes is its own, so parallel tests never step on each other.
func setEnv(t *testing.T, key, val string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, val); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

// The storage-built-in-code table (§16.3): a narrow string holding a bad byte,
// a zero byte and control bytes; a wide string holding an unpaired surrogate
// and a zero unit; floats at the smallest subnormal, at -0, and at the largest
// finite float32; and a bytes field holding a zero byte and a bad byte.
const strictTableSchema = `package probe
fixed table Root {
	s string(8)
	w wstring(8)
	f float64
	g float32
	b bytes(4)
}
`

// strictStorageProbeFor returns the storage probe with the two output paths
// baked in as constants, so the parallel tests never share an environment
// variable or a flag namespace.
func strictStorageProbeFor(out, out2 string) string {
	return fmt.Sprintf(`package probe
import (
	"math"
	"os"
	"testing"
)
const outPath = %q
const out2Path = %q
func writeJSON(t *testing.T, v *Root, path string) {
	t.Helper()
	n := RootToJsonMeasure(v)
	if n < 0 { t.Fatalf("measure refused") }
	text := make([]byte, n)
	if RootToJson(v, text) != int64(len(text)) { t.Fatalf("tojson short") }
	if err := os.WriteFile(path, text, 0o600); err != nil { t.Fatal(err) }
}
func TestStrictStorage(t *testing.T) {
	var v Root
	RootReset(&v)
	v.SLength = 4
	v.S[0], v.S[1], v.S[2], v.S[3] = 0xff, 0x00, 0x1f, 0x7f
	v.WLength = 2
	v.W[0], v.W[1] = 0xd800, 0x0000
	v.F = math.SmallestNonzeroFloat64
	v.G = math.MaxFloat32
	v.BLength = 2
	v.B[0], v.B[1] = 0x00, 0xff
	writeJSON(t, &v, outPath)
	RootReset(&v)
	v.F = -0.0
	v.G = math.MaxFloat32
	writeJSON(t, &v, out2Path)
}
`, out, out2)
}

// TestGoJsonWriterIsStrict holds every JSON text the Go side writes to strict
// RFC 8259 over three sources: every conformance text, the text differential's
// 200 random instances (both the generated ToJson and the engine's text), and
// storage built in code with the §16.3 defect bytes.
func TestGoJsonWriterIsStrict(t *testing.T) {
	t.Parallel()

	// (a) every conformance text on disk
	confDir := "../../../testdata/conformance/tables/json"
	entries, err := os.ReadDir(confDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 92 {
		t.Errorf("conformance json dir: want 92 files, got %d", len(entries))
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(confDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := strictJSON(data); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}

	// (b) the text differential's 200 random instances: the generated ToJson
	// and the engine's text off the same wire.
	probeDir := t.TempDir()
	setEnv(t, "SCHEMA_TEXT_DIFF_DIR", probeDir)
	setEnv(t, "SCHEMA_TEXT_DIFF_SEED", strconv.Itoa(textDifferentialSeed))
	setEnv(t, "SCHEMA_TEXT_DIFF_N", strconv.Itoa(textDifferentialN))
	out, err := runGeneratedEdited(t, textDifferentialSchema, textDifferentialProbe, nil)
	if err != nil {
		t.Fatalf("text differential probe: %v\n%s", err, out)
	}
	m := tabletext.NewModel(unitFrom(t, textDifferentialSchema))
	for i := range textDifferentialN {
		legJSON, err := os.ReadFile(filepath.Join(probeDir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := strictJSON(legJSON); err != nil {
			t.Errorf("instance %d leg json: %v", i, err)
		}
		wire, err := os.ReadFile(filepath.Join(probeDir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			t.Fatal(err)
		}
		d1, err := os.MkdirTemp(probeDir, "d1-")
		if err != nil {
			t.Fatal(err)
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			os.RemoveAll(d1)
			t.Fatalf("instance %d: UnpackOneFile: %v", i, err)
		}
		if report != (tabletext.Report{}) {
			os.RemoveAll(d1)
			t.Fatalf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		os.RemoveAll(d1)
		if err != nil {
			t.Fatal(err)
		}
		if err := strictJSON(engineJSON); err != nil {
			t.Errorf("instance %d engine json: %v", i, err)
		}
	}

	// (c) storage built in code
	cOut := filepath.Join(probeDir, "strict-out.json")
	cOut2 := filepath.Join(probeDir, "strict-out2.json")
	out, err = runGeneratedEdited(t, strictTableSchema, strictStorageProbeFor(cOut, cOut2), nil)
	if err != nil {
		t.Fatalf("storage probe: %v\n%s", err, out)
	}
	for _, p := range []string{cOut, cOut2} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := strictJSON(data); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	// §16.3: the bad byte and the unpaired surrogate both write U+FFFD (EF BF BD)
	data, err := os.ReadFile(cOut)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("\xef\xbf\xbd")) {
		t.Errorf("storage text does not hold U+FFFD (EF BF BD) where the bad byte and unpaired surrogate were: %q", data)
	}
}

// TestGoJsonWriterIsStrictNegativeControl proves the gate is red when the
// writers stop being strict: it refuses a battery of non-strict texts with a
// named offence, and it reruns the storage probe with the bad-byte escape
// sabotaged to write the raw byte, so the text carries invalid UTF-8.
func TestGoJsonWriterIsStrictNegativeControl(t *testing.T) {
	t.Parallel()

	negatives := []struct{ text, want string }{
		{`{"a":1,}` + "\n", "trailing comma"},
		{`{"a":1}//` + "\n", "comment"},
		{`{"a":NaN}` + "\n", "NaN"},
		{`{"a":"\ud800"}` + "\n", "surrogate"},
		{`{"a":"A"}`, "newline"},
		{`{"a":"\/"}` + "\n", "slash"},
		{`{"a":"` + string([]byte{0xff}) + `"}` + "\n", "UTF-8"},
		{`{"a":1}`, "newline"},
	}
	for i, tc := range negatives {
		err := strictJSON([]byte(tc.text))
		if err == nil {
			t.Errorf("negative %d: strictJSON accepted %q", i, tc.text)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("negative %d: offence %q not named in %q", i, tc.want, err.Error())
		}
	}

	// PLANTED WRITER BUG: rerun source (c) with the bad-byte escape replaced by
	// the raw byte, so the text carries invalid UTF-8.
	const anchor = `out.text("�") // one per bad byte`
	cOut := filepath.Join(t.TempDir(), "strict-sabotaged.json")
	cOut2 := filepath.Join(t.TempDir(), "strict-sabotaged2.json")
	out, err := runGeneratedEdited(t, strictTableSchema, strictStorageProbeFor(cOut, cOut2), func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += strings.Count(string(data), anchor)
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", anchor, total)
		}
		patch(t, files, anchor, `out.put(c) /* SABOTAGED */`)
	})
	if err != nil {
		t.Fatalf("sabotaged storage probe: %v\n%s", err, out)
	}
	data, err := os.ReadFile(cOut)
	if err != nil {
		t.Fatal(err)
	}
	err = strictJSON(data)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a writer that emits a raw bad byte left strictJSON GREEN\n%s", data)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "utf-8") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on invalid UTF-8: %v\n%s", err, data)
	}
}
