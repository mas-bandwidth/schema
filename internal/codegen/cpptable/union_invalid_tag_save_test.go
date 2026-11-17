package cpptable

// TestCppUnionListSaveInvalidTagFails pins the failure value of a union's
// invalid tag inside a bool-returning save: a list element and a bounded array
// element whose tag no arm names must make the generated public Save report
// failure, not success. The union pre-measure runs in the middle of the save,
// so a `return -1;` there is `return true;` in a bool function and the save
// carries on having written nothing.
//
// TestCppBoolFunctionsNeverReturnMinusOne is the static stand-in for MSVC
// /W4's C4305 (truncation from int to bool): no function the generated
// Table.h declares to return bool may contain `return -1;`.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppUnionInvalidTagSchema = `package uniontag

fixed table Point
{
    x int32
    y int32
}

union Choice
{
    point  Point
    number int32
}

table Record
{
    changes []Choice
}

fixed table Bounded
{
    slots [2]Choice
}
`

const cppUnionInvalidTagMain = `#include <cstdio>
#include <cstdint>

int main( void )
{
    // A LIST element with a tag no arm names reports failure.
    {
        RecordBuilder b;
        Record * record = b.GetRoot();
        Choice * choice = RecordChangesAdd( b.main, record->changes );
        choice->type = (ChoiceType) 99;
        static uint8_t wire[1u << 16];
        const int64_t n = RecordSave( b, wire, (int64_t) sizeof( wire ) );
        if ( n != -1 )
        {
            std::printf( "list invalid tag: saved as %lld, want -1\n", (long long) n );
            return 1;
        }
    }

    // The same list with a valid tag saves.
    {
        RecordBuilder b;
        Record * record = b.GetRoot();
        Choice * choice = RecordChangesAdd( b.main, record->changes );
        choice->type = ChoiceType::Number;
        choice->number = 42;
        static uint8_t wire[1u << 16];
        const int64_t n = RecordSave( b, wire, (int64_t) sizeof( wire ) );
        if ( n <= 0 )
        {
            std::printf( "list valid tag: saved as %lld, want success\n", (long long) n );
            return 1;
        }
    }

    // A BOUNDED ARRAY element with a tag no arm names reports failure.
    {
        Bounded v;
        BoundedReset( v );
        v.slots[0].type = (ChoiceType) 99;
        static uint8_t wire[1u << 16];
        const int64_t n = BoundedSave( v, wire, (int64_t) sizeof( wire ) );
        if ( n != -1 )
        {
            std::printf( "array invalid tag: saved as %lld, want -1\n", (long long) n );
            return 1;
        }
    }

    // The same bounded array with a valid tag saves.
    {
        Bounded v;
        BoundedReset( v );
        v.slots[0].type = ChoiceType::Number;
        v.slots[0].number = 7;
        static uint8_t wire[1u << 16];
        const int64_t n = BoundedSave( v, wire, (int64_t) sizeof( wire ) );
        if ( n <= 0 )
        {
            std::printf( "array valid tag: saved as %lld, want success\n", (long long) n );
            return 1;
        }
    }

    return 0;
}
`

func cppUnionInvalidTagRun(t *testing.T) ([]byte, error) {
	t.Helper()
	u := cppUnitFrom(t, cppUnionInvalidTagSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(name, "Table.h"):
			header = name
		case strings.HasSuffix(name, ".cpp"):
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := `#include <cstdio>
#include <cstdint>
#include "` + header + `"
using namespace uniontag;
` + cppUnionInvalidTagMain
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-Wall", "-Wextra", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func TestCppUnionListSaveInvalidTagFails(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	out, err := cppUnionInvalidTagRun(t)
	if err != nil {
		t.Fatalf("a union element with an invalid tag saved successfully: %v\n%s", err, out)
	}
}

// maskCpp blanks comments and string and character literals, preserving every
// position and newline, so a brace walk over the result sees only code.
func maskCpp(text string) string {
	b := []byte(text)
	out := append([]byte(nil), b...)
	const (
		normal = iota
		lineComment
		blockComment
		stringLiteral
		charLiteral
	)
	state := normal
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case normal:
			switch {
			case c == '/' && i+1 < len(b) && b[i+1] == '/':
				out[i], out[i+1] = ' ', ' '
				i++
				state = lineComment
			case c == '/' && i+1 < len(b) && b[i+1] == '*':
				out[i], out[i+1] = ' ', ' '
				i++
				state = blockComment
			case c == '"':
				out[i] = ' '
				state = stringLiteral
			case c == '\'':
				out[i] = ' '
				state = charLiteral
			}
		case lineComment:
			if c == '\n' {
				state = normal
			} else {
				out[i] = ' '
			}
		case blockComment:
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		case stringLiteral:
			if c == '\\' && i+1 < len(b) {
				out[i], out[i+1] = ' ', ' '
				i++
			} else if c == '"' {
				out[i] = ' '
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		case charLiteral:
			if c == '\\' && i+1 < len(b) {
				out[i], out[i+1] = ' ', ' '
				i++
			} else if c == '\'' {
				out[i] = ' '
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

func TestCppBoolFunctionsNeverReturnMinusOne(t *testing.T) {
	u := cppUnitFrom(t, cppUnionInvalidTagSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	var headerName string
	var header []byte
	for name, data := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			headerName, header = name, data
		}
	}
	if headerName == "" {
		t.Fatal("no Table.h was generated")
	}

	masked := maskCpp(string(header))
	boolDef := regexp.MustCompile(`\bbool\s+([A-Za-z_]\w*)\s*\(`)
	minusOne := regexp.MustCompile(`\breturn\s+-1\s*;`)
	for _, m := range boolDef.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[m[2]:m[3]]
		open := strings.LastIndex(masked[m[0]:m[1]], "(") + m[0]
		depth := 0
		at := open
		for ; at < len(masked); at++ {
			if masked[at] == '(' {
				depth++
			} else if masked[at] == ')' {
				depth--
				if depth == 0 {
					at++
					break
				}
			}
		}
		for at < len(masked) && (masked[at] == ' ' || masked[at] == '\t' || masked[at] == '\r' || masked[at] == '\n') {
			at++
		}
		if at >= len(masked) || masked[at] != '{' {
			continue // a declaration, not a definition
		}
		bodyStart := at
		bodyEnd := -1
		depth = 0
		for ; at < len(masked); at++ {
			if masked[at] == '{' {
				depth++
			} else if masked[at] == '}' {
				depth--
				if depth == 0 {
					bodyEnd = at
					break
				}
			}
		}
		if bodyEnd < 0 {
			t.Fatalf("%s: bool %s has no closing brace", headerName, name)
		}
		if loc := minusOne.FindStringIndex(masked[bodyStart:bodyEnd]); loc != nil {
			line := 1 + strings.Count(masked[:bodyStart+loc[0]], "\n")
			t.Errorf("%s:%d: bool %s returns -1; the failure path must return the caller's false", headerName, line, name)
		}
	}
}
