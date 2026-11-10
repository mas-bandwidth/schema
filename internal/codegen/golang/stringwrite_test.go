package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoStringWriteRefusesInteriorNULAndInvalidUTF8 pins the string(N) packet
// writer. An interior NUL and a payload that is not UTF-8 are each refused
// with serialize.ErrValueOutOfRange, and both checks sit before the used
// bytes are written. bytes(N) keeps the same framing with neither rule.
func TestGoStringWriteRefusesInteriorNULAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	files := generateGo(t, "Note", "package note\n\ntype Note {\n    text string(8)\n}\n")
	body := generatedFunc(t, files, "func WriteNote(")
	nul := strings.Index(body, "unit == 0")
	utf := strings.Index(body, "!utf8.Valid(")
	payload := strings.Index(body, "stream.SerializeBytes(")
	if nul < 0 || utf < 0 || payload < 0 || nul > payload || utf > payload {
		t.Fatalf("WriteNote does not refuse an interior NUL and invalid UTF-8 before the string bytes:\n%s", body)
	}
	if got := strings.Count(body, "return serialize.ErrValueOutOfRange"); got < 2 {
		t.Fatalf("WriteNote returns serialize.ErrValueOutOfRange %d times, want the length check plus both content refusals:\n%s", got, body)
	}

	blobs := generateGo(t, "Blob", "package blob\n\ntype Blob {\n    data bytes(8)\n}\n")
	blob := generatedFunc(t, blobs, "func WriteBlob(")
	if strings.Contains(blob, "utf8.Valid") || strings.Contains(blob, "unit == 0") {
		t.Fatalf("bytes(N) write refused string content:\n%s", blob)
	}

	runStringWriteRefusalProbe(t, files)
}

// generatedFunc returns one emitted function, braces and all, so a check can
// see past the first inner block (funcBody stops there).
func generatedFunc(t *testing.T, files map[string][]byte, decl string) string {
	t.Helper()
	for _, src := range files {
		text := string(src)
		start := strings.Index(text, decl)
		if start < 0 {
			continue
		}
		rest := text[start:]
		open := strings.Index(rest, "{")
		if open < 0 {
			t.Fatalf("%s has no body", decl)
		}
		depth := 0
		for i := open; i < len(rest); i++ {
			switch rest[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return rest[:i+1]
				}
			}
		}
		t.Fatalf("%s body does not close", decl)
	}
	t.Fatalf("%s was not emitted", decl)
	return ""
}

// runStringWriteRefusalProbe compiles the generated writer and runs it. An
// interior NUL is well-formed UTF-8, so each refusal is its own case; a
// legal payload still writes. Behind slowtest.Gate — it shells out to the
// Go toolchain.
func runStringWriteRefusalProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")

	const probe = `package note

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestWriteRefusesBadText(t *testing.T) {
	refuse := func(label string, text []byte) {
		t.Helper()
		var note Note
		copy(note.Text[:], text)
		note.TextLength = int32(len(text))
		stream := serialize.NewWriteStream(make([]byte, 64))
		err := WriteNote(stream, &note)
		if !errors.Is(err, serialize.ErrValueOutOfRange) {
			t.Fatalf("%s: got %v, want serialize.ErrValueOutOfRange", label, err)
		}
	}
	refuse("interior NUL", []byte{'a', 0, 'b'})
	refuse("invalid UTF-8", []byte{0xff})

	var note Note
	copy(note.Text[:], "ok")
	note.TextLength = 2
	stream := serialize.NewWriteStream(make([]byte, 64))
	if err := WriteNote(stream, &note); err != nil {
		t.Fatalf("legal text: %v", err)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["stringwrite_probe_test.go"] = []byte(probe)

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its probe: %v\n%s", err, out)
	}
}
