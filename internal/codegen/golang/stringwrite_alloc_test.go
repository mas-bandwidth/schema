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

// TestGoStringWriteCheckAllocatesNothing pins the string(N) writer's UTF-8
// check to the byte slice it already holds. Converting the slice to a string
// first copies it to the heap once it is longer than the runtime's small
// buffer, so a write of a long string would allocate where the packet write is
// otherwise allocation-free.
func TestGoStringWriteCheckAllocatesNothing(t *testing.T) {
	t.Parallel()
	files := generateGo(t, "Line", "package line\n\ntype Line {\n    text string(64)\n}\n")
	body := generatedFunc(t, files, "func WriteLine(")
	if strings.Contains(body, "string(value.Text[:") || !strings.Contains(body, "!utf8.Valid(value.Text[:value.TextLength])") {
		t.Errorf("WriteLine checks UTF-8 through a string conversion, not on the bytes:\n%s", body)
	}

	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")
	const probe = `package line

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestWriteLongTextAllocatesNothing(t *testing.T) {
	var line Line
	text := "a line of text longer than thirty-two bytes"
	copy(line.Text[:], text)
	line.TextLength = int32(len(text))
	buffer := make([]byte, 128)
	stream := serialize.NewWriteStream(buffer)
	allocs := testing.AllocsPerRun(100, func() {
		stream.Reset(buffer)
		if err := WriteLine(stream, &line); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("WriteLine of %d bytes: %v allocs/op, want 0", len(text), allocs)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["stringwrite_alloc_probe_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its allocation probe: %v\n%s", err, out)
	}
}
