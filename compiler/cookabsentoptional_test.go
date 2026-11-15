// The GO COOK AND THE C COOK AGREE ON AN ABSENT OPTIONAL (docs/SPEC-TABLES.md
// §7.2; docs/rulings.json `fixed-dead-bytes-are-zero`). The payload of an
// absent optional is dead bytes — zero on write — and the element's declared
// default is a READ rule that never enters the dead storage. This holds the two
// writers to one file, byte for byte, in both byte orders: the Go reference
// cook (internal/tablecook) and the C cook of the same fixed table.
package compiler

import (
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	ctable "github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const absentOptionalPayloadSchema = `package probe

type Pose
{
    x float32 = 0.5
}

fixed table Root
{
    spawn ?Pose
}
`

const absentOptionalPayloadDriver = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>

static int one( int order )
{
    Root v;
    int64_t n;
    static uint8_t cooked[ 256 ];
    uint64_t i;
    root_reset( &v );
    n = root_cook_measure( &v );
    if ( n <= 0 || n > (int64_t) sizeof( cooked ) ) { return 1; }
    if ( !root_cook( &v, cooked, (uint64_t) n, order ) ) { return 1; }
    for ( i = 0; i < (uint64_t) n; i++ ) { printf( "%02x", cooked[i] ); }
    printf( "\n" );
    return 0;
}

int main( void )
{
    if ( one( TableByteOrder_Little ) ) { return 1; }
    if ( one( TableByteOrder_Big ) ) { return 1; }
    return 0;
}
`

func TestCookAbsentOptionalPayloadZero(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	u := unitFromSource(t, absentOptionalPayloadSchema)

	// the GO cook, in both byte orders, of the value whose optional is absent
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Root"))
	var want [2]string
	for i, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(model, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: go cook: %v", big, err)
		}
		want[i] = hex.EncodeToString(cooked)
	}

	// the C cook of the same value, compiled and run
	cfiles, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tfiles, err := ctable.Generate(u)
	if err != nil {
		t.Fatalf("ctable: %v", err)
	}
	maps.Copy(cfiles, tfiles)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("the generated cook path is C: no cc on PATH")
	}
	dir := t.TempDir()
	for name, data := range cfiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(absentOptionalPayloadDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for name := range cfiles {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the C cook refused its own value: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the driver printed %d lines, want one cook per byte order:\n%s", len(lines), out)
	}
	for i, name := range []string{"little-endian", "big-endian"} {
		if lines[i] != want[i] {
			t.Fatalf("%s: the C cook and the Go cook differ\n  go: %s\n   c: %s", name, want[i], lines[i])
		}
	}
}
