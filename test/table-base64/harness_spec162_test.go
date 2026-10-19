package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	b64cases "github.com/mas-bandwidth/schema/v2/test/table-base64/cases"
)

// TestMain lets the test binary stand in for a port's driver. The reversed
// witness below runs the real harness against a driver that answers from the
// standard's rule, then against a copy of the harness whose Q=Q== expectation
// has been flipped back to kind_mismatch.
func TestMain(m *testing.M) {
	if os.Getenv("SCHEMA_BASE64_FAKE_DRIVER") == "1" {
		runFakeDriver()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeDriver() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 65536), 1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer func() {
		if err := out.Flush(); err != nil {
			os.Exit(1)
		}
	}()
	for in.Scan() {
		raw, err := hex.DecodeString(in.Text())
		if err != nil {
			fmt.Fprintln(out, "1 0 0 - -")
			continue
		}
		var doc struct {
			Payload *string `json:"payload"`
		}
		if !utf8.Valid(raw) {
			fmt.Fprintln(out, "1 0 0 - -")
			continue
		}
		if err := json.Unmarshal(raw, &doc); err != nil || doc.Payload == nil {
			fmt.Fprintln(out, "1 0 0 - -")
			continue
		}
		payload, mismatch, clamped := specBase64(*doc.Payload)
		decoded := hex.EncodeToString(payload)
		if decoded == "" {
			decoded = "-"
		}
		written, err := json.Marshal(struct {
			Payload string `json:"payload"`
		}{base64.StdEncoding.EncodeToString(payload)})
		if err != nil {
			os.Exit(1)
		}
		fmt.Fprintf(out, "0 %d %d %s %s\n", clamped, mismatch, decoded, hex.EncodeToString(written))
	}
	if err := in.Err(); err != nil {
		os.Exit(1)
	}
}

// specBase64 is the standard's answer for one body (docs/SPEC-TABLES.md
// §16.2 and §16.8): the canonical encoding is accepted; a body that is
// base64 but not canonical is accepted by the tolerant read and reported,
// which the harness sees as zero kind mismatches; anything else is a kind
// mismatch. Length past the declared bound clamps and counts.
func specBase64(body string) (payload []byte, mismatch, clamped int) {
	if decoded, err := base64.StdEncoding.Strict().DecodeString(body); err == nil && base64.StdEncoding.EncodeToString(decoded) == body {
		return clamp(decoded)
	}
	var cleaned strings.Builder
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '=', ' ', '\t', '\r', '\n':
		default:
			cleaned.WriteByte(body[i])
		}
	}
	if cleaned.Len() == 0 {
		return nil, 1, 0
	}
	decoded, err := base64.RawStdEncoding.DecodeString(cleaned.String())
	if err != nil || len(decoded) == 0 {
		return nil, 1, 0
	}
	return clamp(decoded)
}

func clamp(decoded []byte) ([]byte, int, int) {
	if len(decoded) > 16384 {
		return decoded[:16384], 0, 1
	}
	return decoded, 0, 0
}

// TestBase64HarnessFollowsSpec162 holds the harness's case table to
// docs/SPEC-TABLES.md §16.2's bytes(N) row and §16.8's canonical definition.
// Red at BASE: the table expected `Q=Q==` (and `Q=Q=A=A=`, `QQ==Q`) to count
// kind_mismatch, when §16.2 makes a decodable but non-canonical body
// base64_not_canonical on the strict read and an accepted, reported value on
// the tolerant read; and it expected raw NUL and other bytes that are not
// legal JSON string characters to be a kind mismatch rather than malformed.
func TestBase64HarnessFollowsSpec162(t *testing.T) {
	t.Parallel()
	corpus := b64cases.Corpus()
	byName := make(map[string]b64cases.Vector, len(corpus))
	for _, v := range corpus {
		byName[v.Name] = v
	}
	for _, row := range []struct {
		name      string
		want      []byte
		mismatch  int
		malformed bool
		cite      string
	}{
		{
			name:     "invalid-padding-=",
			mismatch: 1,
			cite:     "§16.2 bytes row: a body that is not base64 at all keeps kind_mismatch; a lone `=` has no alphabet symbol",
		},
		{
			name:     "invalid-padding-Q",
			mismatch: 1,
			cite:     "§16.2 bytes row: a body that is not base64 at all keeps kind_mismatch; one symbol cannot form a group",
		},
		{
			name:     "invalid-padding-Q=Q==",
			want:     []byte{0x41},
			mismatch: 0,
			cite:     "§16.2 bytes row: `=` anywhere but the end is base64_not_canonical; the tolerant read accepts it and reports it",
		},
		{
			name:     "invalid-padding-Q=Q=A=A=",
			want:     []byte{0x41, 0x00, 0x00},
			mismatch: 0,
			cite:     "§16.2 bytes row: `=` anywhere but the end is base64_not_canonical; the tolerant read accepts it and reports it",
		},
		{
			name:     "invalid-padding-QQ==Q",
			want:     []byte{0x41, 0x04},
			mismatch: 0,
			cite:     "§16.2 bytes row: `=` anywhere but the end is base64_not_canonical; the tolerant read accepts it and reports it",
		},
		{
			name:     "invalid-20",
			want:     []byte{0x41, 0x00, 0x00},
			mismatch: 0,
			cite:     "§16.2 bytes row: any byte outside the alphabet and `=` including whitespace is base64_not_canonical; the tolerant read accepts it",
		},
	} {
		got, ok := byName[row.name]
		if !ok {
			t.Fatalf("%s: no such case in the harness table", row.name)
		}
		if got.Malformed != row.malformed || got.Mismatch != row.mismatch || !bytes.Equal(got.Want, row.want) {
			t.Fatalf("%s: table says malformed=%t mismatch=%d want=%x, want malformed=%t mismatch=%d want=%x (%s)",
				row.name, got.Malformed, got.Mismatch, got.Want, row.malformed, row.mismatch, row.want, row.cite)
		}
	}

	// The base red: a raw NUL byte is not a legal JSON string character, so
	// the reader answers malformed (docs/SPEC-TABLES.md §16.1's JSON grammar);
	// §16.2's body rule is never reached.
	nul, ok := byName["invalid-00"]
	if !ok || !nul.Malformed {
		t.Fatalf("invalid-00: table says malformed=%t, want true (a raw control byte is not a JSON string, §16.1)", nul.Malformed)
	}

	// The Go oracle answers Q=Q== the same way: the strict generated read
	// refuses it by name base64_not_canonical, and the tolerant read the
	// harness drives accepts it, lands 0x41 and reports the refusal. The
	// reversed witness below proves the harness's expectation is that one.

	// Reversed witness: run the harness's own source against a driver that
	// follows the standard, then against a throwaway copy with the Q=Q==
	// expectation flipped back to kind_mismatch. The real harness is green;
	// the flipped copy goes red naming that case.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	real := filepath.Join(tmp, "harness")
	build := exec.Command("go", "build", "-o", real, "./test/table-base64/harness")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real harness: %v\n%s", err, out)
	}
	if out, err := runHarness(real, exe); err != nil {
		t.Fatalf("harness is red against a spec driver: %v\n%s", err, out)
	}

	srcPath := filepath.Join("cases", "cases.go")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	old := `{"Q=Q==", []byte{0x41}, 0},`
	flipped := `{"Q=Q==", nil, 1},`
	if !bytes.Contains(src, []byte(old)) {
		t.Fatalf("cases.go no longer carries the Q=Q== expectation line")
	}
	flipDir := filepath.Join(tmp, "flip")
	if err := os.MkdirAll(filepath.Join(flipDir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(flipDir, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flipDir, "cases", "cases.go"), bytes.Replace(src, []byte(old), []byte(flipped), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	mainSrc, err := os.ReadFile(filepath.Join("harness", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flipDir, "harness", "main.go"), mainSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	const goMod = "module github.com/mas-bandwidth/schema/v2/test/table-base64\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(flipDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	flipHarness := filepath.Join(tmp, "harness-flipped")
	build = exec.Command("go", "build", "-o", flipHarness, "./harness")
	build.Dir = flipDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build flipped harness: %v\n%s", err, out)
	}
	out, err := runHarness(flipHarness, exe)
	if err == nil {
		t.Fatalf("flipped harness passed; the reversed witness did not fire")
	}
	if !strings.Contains(out, "invalid-padding-Q=Q==") {
		t.Fatalf("flipped harness failed without naming invalid-padding-Q=Q==:\n%s", out)
	}
}

func runHarness(harness, driver string) (string, error) {
	cmd := exec.Command(harness, driver)
	cmd.Env = append(os.Environ(), "SCHEMA_BASE64_FAKE_DRIVER=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
