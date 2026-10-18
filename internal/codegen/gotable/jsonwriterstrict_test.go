package gotable

// The Go text writers (the emitted walk and the engine) must emit STRICT RFC
// 8259 JSON: valid JSON by encoding/json.Valid's grammar (no comment, no
// trailing comma, no NaN or Infinity, no leading zero), valid UTF-8, only the
// seven named short escapes plus a lower-case \u00xx escape for a byte under
// 0x20 (so no \u escape can name a surrogate, and a \/ never appears), and
// exactly one trailing newline (§16.1). JSONTestSuite's y_ files are accepted
// by every conforming parser; a text writer must never land in i_. This file
// holds that over every text the Go side writes.
//
// The card is pure test: no writer change, no standard change. If the test
// found a writer defect it would finish HOLD with the failing text quoted.
// It does not, so this test is the guard (docs/SPEC-TABLES.md §16.1, §16.3).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// strictJSON verifies that text is a STRICT RFC 8259 JSON text: valid UTF-8,
// valid by encoding/json.Valid's grammar, only the seven named short escapes
// or a lower-case \u00xx escape for a byte under 0x20, and exactly one trailing
// newline. The error names the first offence and its byte offset into the text
// under the final newline.
func strictJSON(text []byte) error {
	if len(text) == 0 || text[len(text)-1] != '\n' {
		return fmt.Errorf("text does not end with a single newline at offset %d", len(text))
	}
	if len(text) >= 2 && text[len(text)-2] == '\n' {
		return fmt.Errorf("text ends with more than one newline at offset %d", len(text)-2)
	}
	body := text[:len(text)-1]
	for i := 0; i < len(body); {
		r, size := utf8.DecodeRune(body[i:])
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("invalid UTF-8 at offset %d", i)
		}
		i += size
	}
	if err := strictJSONScan(body); err != nil {
		return err
	}
	if !json.Valid(body) {
		return fmt.Errorf("not valid JSON at offset 0")
	}
	return nil
}

// strictJSONScan walks the text (without the final newline) and names the first
// specific offence it meets: a trailing comma, a comment, a NaN or Infinity
// token, and any escape the writer must never emit.
func strictJSONScan(b []byte) error {
	inStr := false
	for i := 0; i < len(b); {
		c := b[i]
		if inStr {
			switch c {
			case '"':
				inStr = false
				i++
			case '\\':
				if i+1 >= len(b) {
					return fmt.Errorf("a lone backslash at offset %d", i)
				}
				e := b[i+1]
				switch e {
				case '"', '\\', 'b', 'f', 'n', 'r', 't':
					i += 2
				case 'u':
					if i+6 > len(b) {
						return fmt.Errorf("a short \\u escape at offset %d", i)
					}
					off := i
					hexx := b[i+2 : i+6]
					v := 0
					for _, h := range hexx {
						v <<= 4
						switch {
						case h >= '0' && h <= '9':
							v += int(h - '0')
						case h >= 'a' && h <= 'f':
							v += int(h-'a') + 10
						default:
							return fmt.Errorf("a \\u escape with a non-lower-case hex digit at offset %d", off)
						}
					}
					switch {
					case v >= 0xd800 && v <= 0xdfff:
						return fmt.Errorf("a \\u escape naming a surrogate at offset %d", off)
					case v >= 0x20:
						return fmt.Errorf("a \\u escape naming a byte >= 0x20 at offset %d", off)
					}
					i += 6
				default:
					return fmt.Errorf("an escaped character %q at offset %d", e, i+1)
				}
			default:
				i++
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			i++
		case '/':
			if i+1 < len(b) && (b[i+1] == '/' || b[i+1] == '*') {
				return fmt.Errorf("a comment at offset %d", i)
			}
			i++
		case ',':
			j := i + 1
			for j < len(b) && (b[j] == ' ' || b[j] == '\t' || b[j] == '\n' || b[j] == '\r') {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				return fmt.Errorf("a trailing comma at offset %d", i)
			}
			i++
		case 'N':
			if i+3 <= len(b) && string(b[i:i+3]) == "NaN" {
				return fmt.Errorf("NaN at offset %d", i)
			}
			i++
		case 'I':
			if i+8 <= len(b) && string(b[i:i+8]) == "Infinity" {
				return fmt.Errorf("Infinity at offset %d", i)
			}
			i++
		case '-':
			if i+9 <= len(b) && string(b[i:i+9]) == "-Infinity" {
				return fmt.Errorf("Infinity at offset %d", i)
			}
			i++
		default:
			i++
		}
	}
	return nil
}

const storageStrictSchema = `package probe
fixed table Root {
	s string(8)
	w wstring(8)
	f float64
	g float32
	b bytes(4)
}
`

const storageStrictProbe = `package probe

import (
	"math"
	"os"
	"testing"
)

func writeStorage(t *testing.T, name string, v *Root) {
	dir := os.Getenv("SCHEMA_TEXT_DIFF_DIR")
	if dir == "" {
		t.Fatal("SCHEMA_TEXT_DIFF_DIR is unset")
	}
	n := RootToJsonMeasure(v)
	if n < 0 {
		t.Fatalf("%s: measure refused", name)
	}
	out := make([]byte, n)
	if RootToJson(v, out) != n {
		t.Fatalf("%s: write short", name)
	}
	if err := os.WriteFile(dir+"/"+name+".json", out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStorage(t *testing.T) {
	fill := func(v *Root) {
		v.S[0] = 0xff
		v.S[1] = 0x00
		v.S[2] = 0x1f
		v.S[3] = 0x7f
		v.SLength = 4
		v.W[0] = 0xd800
		v.W[1] = 0
		v.WLength = 2
		v.G = math.MaxFloat32
		v.B[0] = 0x00
		v.B[1] = 0xff
		v.BLength = 2
	}
	var v0 Root
	RootReset(&v0)
	fill(&v0)
	v0.F = math.SmallestNonzeroFloat64
	writeStorage(t, "out0", &v0)
	var v1 Root
	RootReset(&v1)
	fill(&v1)
	v1.F = math.Copysign(0, -1)
	writeStorage(t, "out1", &v1)
}
`

// runStorageStrict runs the built-storage probe, optionally with an in-memory
// sabotage of the emitted files, and returns the directory holding out0.json
// and out1.json.
func runStorageStrict(t *testing.T, edit func(map[string][]byte)) string {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("SCHEMA_TEXT_DIFF_DIR", dir)
	defer os.Unsetenv("SCHEMA_TEXT_DIFF_DIR")
	out, err := runGeneratedEdited(t, storageStrictSchema, storageStrictProbe, edit)
	if err != nil {
		t.Fatalf("storage probe: %v\n%s", err, out)
	}
	return dir
}

// TestGoJsonWriterIsStrict holds the writers strict over every text the Go
// side writes: the conformance corpus, the text differential's 200 random
// instances (both the generated ToJson and the engine's unpack text), and
// storage built in code whose ill-formed bytes must land as U+FFFD.
func TestGoJsonWriterIsStrict(t *testing.T) {
	t.Parallel()

	t.Run("conformance", func(t *testing.T) {
		entries, err := os.ReadDir("../../../testdata/conformance/tables/json")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join("../../../testdata/conformance/tables/json", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := strictJSON(data); err != nil {
				t.Fatalf("conformance %s not strict: %v", e.Name(), err)
			}
		}
	})

	t.Run("textdifferential", func(t *testing.T) {
		dir := t.TempDir()
		os.Setenv("SCHEMA_TEXT_DIFF_DIR", dir)
		defer os.Unsetenv("SCHEMA_TEXT_DIFF_DIR")
		os.Setenv("SCHEMA_TEXT_DIFF_SEED", "24845619678")
		defer os.Unsetenv("SCHEMA_TEXT_DIFF_SEED")
		os.Setenv("SCHEMA_TEXT_DIFF_N", "200")
		defer os.Unsetenv("SCHEMA_TEXT_DIFF_N")
		out, err := runGeneratedEdited(t, textDifferentialSchema, textDifferentialProbe, nil)
		if err != nil {
			t.Fatalf("text differential probe: %v\n%s", err, out)
		}
		u := unitFrom(t, textDifferentialSchema)
		m := tabletext.NewModel(u)
		for i := range textDifferentialN {
			legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
			if err != nil {
				t.Fatal(err)
			}
			if err := strictJSON(legJSON); err != nil {
				t.Fatalf("instance %d leg text not strict: %v\n%s", i, err, legJSON)
			}
			wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
			if err != nil {
				t.Fatal(err)
			}
			d := t.TempDir()
			report, err := tablepack.UnpackOneFile(m, "Root", wire, d)
			if err != nil {
				t.Fatalf("instance %d: UnpackOneFile: %v", i, err)
			}
			if report != (tabletext.Report{}) {
				t.Fatalf("instance %d: UnpackOneFile report not clean: %+v", i, report)
			}
			engineJSON, err := os.ReadFile(filepath.Join(d, "Root.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := strictJSON(engineJSON); err != nil {
				t.Fatalf("instance %d engine text not strict: %v\n%s", i, err, engineJSON)
			}
		}
	})

	t.Run("storage", func(t *testing.T) {
		dir := runStorageStrict(t, nil)
		for _, name := range []string{"out0", "out1"} {
			data, err := os.ReadFile(filepath.Join(dir, name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := strictJSON(data); err != nil {
				t.Fatalf("%s not strict: %v\n%s", name, err, data)
			}
			if n := bytes.Count(data, []byte("\xef\xbf\xbd")); n != 2 {
				t.Fatalf("%s: %d U+FFFD, want 2 (one for the bad byte, one for the unpaired surrogate)", name, n)
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, "out1.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("-0")) {
			t.Fatalf("out1: float64 -0 did not write as -0:\n%s", data)
		}
	})
}

// TestGoJsonWriterIsStrictNegativeControl has two halves: strictJSON refuses
// each malformed text with its offence named, and a planted writer bug — the
// U+FFFD replacement swapped for the raw byte — goes red on invalid UTF-8,
// never on a compile error.
func TestGoJsonWriterIsStrictNegativeControl(t *testing.T) {
	t.Parallel()

	refusals := []struct {
		name string
		text string
		want string
	}{
		{"trailing comma", "{\"a\":1,}\n", "trailing comma"},
		{"comment", "{\"a\":1}//\n", "comment"},
		{"NaN", "{\"a\":NaN}\n", "NaN"},
		{"surrogate escape", "{\"a\":\"\\ud800\"}\n", "surrogate"},
		{"escape naming byte >= 0x20", "{\"a\":\"\\u0041\"}\n", ">= 0x20"},
		{"escaped slash", "{\"a\":\"\\/\"}\n", "escaped"},
		{"invalid UTF-8", "{\"a\":\"\xff\"}\n", "invalid UTF-8"},
		{"no final newline", "{\"a\":1}", "newline"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			err := strictJSON([]byte(tc.text))
			if err == nil {
				t.Fatalf("strictJSON accepted %q", tc.text)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("strictJSON(%q) = %q, want it to name %q", tc.text, err, tc.want)
			}
		})
	}

	t.Run("sabotaged-writer", func(t *testing.T) {
		anchor := "out.text(\"�\") // one per bad byte"
		dir := runStorageStrict(t, func(files map[string][]byte) {
			total := 0
			for _, data := range files {
				total += strings.Count(string(data), anchor)
			}
			if total != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", anchor, total)
			}
			patch(t, files, anchor, "out.put(c) /* SABOTAGED */")
		})
		data, err := os.ReadFile(filepath.Join(dir, "out0.json"))
		if err != nil {
			t.Fatal(err)
		}
		err = strictJSON(data)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotaged writer left the gate GREEN\n%s", data)
		}
		if !strings.Contains(err.Error(), "invalid UTF-8") {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on invalid UTF-8: %v", err)
		}
	})
}
