package cpptable

// A text writer never produces text its own reader refuses (docs/SPEC-TABLES.md
// §16.14 W0, W8; docs/SPEC.md §5.1 row 8). The C++ ToJson reader refuses a
// placed U+0000, so the writer refuses a string, a text-list element or a map
// key that holds a zero byte, by the same out.invalid path it already takes
// for ill-formed UTF-8. The escapes U+0001 through U+001F are ordinary text
// and still write as \u0001 and \u001f. A value without the zero byte writes
// and reads back unchanged.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppJsonZeroSchema = `package probe

table Root
{
    s string(8)
    names [..3]string(8)
    by_name map[string(8)]int32
}
`

const cppJsonZeroProbe = `#include <stdio.h>
#include <string.h>
#include "%s"

using namespace probe;

static void Print( const char * name, const Root * value, char * out, int64_t capacity )
{
    const int64_t measure = RootToJsonMeasure( value );
    const int64_t written = measure < 0 ? -1 : RootToJson( value, out, capacity );
    bool read = false;
    char again[1 << 16];
    int64_t reread = -1;
    if ( written >= 0 )
    {
        RootBuilder builder;
        TableReport report;
        read = RootFromJson( builder, out, written, &report );
        if ( read && builder.Lock() ) { reread = RootToJson( builder.AsConst(), again, (int64_t) sizeof( again ) ); }
    }
    printf( "RESULT %%s %%lld %%lld %%d ", name, (long long) measure, (long long) written, read ? 1 : 0 );
    if ( written < 0 ) { printf( "-" ); }
    else
    {
        for ( int64_t i = 0; i < written; i++ ) { printf( "%%02x", (unsigned char) out[i] ); }
    }
    printf( " " );
    if ( reread < 0 ) { printf( "-" ); }
    else
    {
        for ( int64_t i = 0; i < reread; i++ ) { printf( "%%02x", (unsigned char) again[i] ); }
    }
    printf( "\n" );
}

static void PutText( char * to, int32_t * length, const char * text, int32_t bytes )
{
    memset( to, 0, 9 );
    memcpy( to, text, (size_t) bytes );
    *length = bytes;
}

int main( void )
{
    static const char zero[3] = { 'a', 0, 'b' };
    static const char control[4] = { 'a', 1, 31, 'b' };
    static const char clean[2] = { 'a', 'b' };
    char out[1 << 16];

    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->s, &value->s_length, zero, 3 );
        if ( !builder.Lock() ) { fprintf( stderr, "field-zero: Lock failed\\n" ); return 1; }
        Print( "field-zero", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->names[0].value, &value->names[0].value_length, zero, 3 );
        value->names_count = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "list-zero: Lock failed\\n" ); return 1; }
        Print( "list-zero", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        TableWorker worker = builder.Worker();
        RootByNameEntry * entry = TableMapPlace( worker, value->by_name, TableMapKeyRef{ zero, 3 } );
        entry->value = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "key-zero: Lock failed\\n" ); return 1; }
        Print( "key-zero", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->s, &value->s_length, control, 4 );
        if ( !builder.Lock() ) { fprintf( stderr, "field-control: Lock failed\\n" ); return 1; }
        Print( "field-control", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->names[0].value, &value->names[0].value_length, control, 4 );
        value->names_count = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "list-control: Lock failed\\n" ); return 1; }
        Print( "list-control", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        TableWorker worker = builder.Worker();
        RootByNameEntry * entry = TableMapPlace( worker, value->by_name, TableMapKeyRef{ control, 4 } );
        entry->value = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "key-control: Lock failed\\n" ); return 1; }
        Print( "key-control", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->s, &value->s_length, clean, 2 );
        if ( !builder.Lock() ) { fprintf( stderr, "field-clean: Lock failed\\n" ); return 1; }
        Print( "field-clean", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        PutText( value->names[0].value, &value->names[0].value_length, clean, 2 );
        value->names_count = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "list-clean: Lock failed\\n" ); return 1; }
        Print( "list-clean", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    {
        RootBuilder builder;
        Root * value = builder.GetRoot();
        TableWorker worker = builder.Worker();
        RootByNameEntry * entry = TableMapPlace( worker, value->by_name, TableMapKeyRef{ clean, 2 } );
        entry->value = 1;
        if ( !builder.Lock() ) { fprintf( stderr, "key-clean: Lock failed\\n" ); return 1; }
        Print( "key-clean", builder.AsConst(), out, (int64_t) sizeof( out ) );
    }
    return 0;
}
`

type cppJsonZeroAnswer struct {
	measure int64
	written int64
	read    bool
	text    string
	reread  string
}

func runCppJsonZeroProbe(t *testing.T) []byte {
	t.Helper()
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated unit)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("%s: %v", cxx, err)
	}

	u := cppUnitFrom(t, cppJsonZeroSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)

	src := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(src, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(src, name))
		}
	}
	if len(sources) == 0 {
		t.Fatal("Generate emitted no table source for the probe unit")
	}
	probe := filepath.Join(src, "probe.cpp")
	if err := os.WriteFile(probe, []byte(fmt.Sprintf(cppJsonZeroProbe, "ProbeTable.h")), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + src, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe).CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ probe failed: %v\n%s", err, out)
	}
	return out
}

func TestCppJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()

	out := runCppJsonZeroProbe(t)

	got := map[string]cppJsonZeroAnswer{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		var name, text, reread string
		var measure, written int64
		var read int
		if _, err := fmt.Sscanf(line, "RESULT %s %d %d %d %s %s", &name, &measure, &written, &read, &text, &reread); err != nil {
			t.Fatalf("parse probe line %q: %v", line, err)
		}
		got[name] = cppJsonZeroAnswer{measure: measure, written: written, read: read != 0, text: text, reread: reread}
	}

	decode := func(t *testing.T, name, encoded string) []byte {
		t.Helper()
		if encoded == "-" {
			return nil
		}
		raw, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatalf("%s: decode probe bytes: %v", name, err)
		}
		return raw
	}

	for _, name := range []string{"field-zero", "list-zero", "key-zero"} {
		answer, seen := got[name]
		if !seen {
			t.Errorf("%s: the C++ probe reported no answer", name)
			continue
		}
		if answer.measure >= 0 || answer.written >= 0 {
			t.Errorf("%s: a zero byte in text must refuse both measure and write, got measure=%d write=%d",
				name, answer.measure, answer.written)
		}
		if answer.text != "-" {
			t.Errorf("%s: a refused write produced text %q", name, answer.text)
		}
	}

	for _, name := range []string{"field-control", "list-control", "key-control"} {
		answer, seen := got[name]
		if !seen {
			t.Errorf("%s: the C++ probe reported no answer", name)
			continue
		}
		if answer.measure < 0 || answer.written != answer.measure {
			t.Errorf("%s: a control byte must write at its measured length, got measure=%d write=%d",
				name, answer.measure, answer.written)
			continue
		}
		text := decode(t, name, answer.text)
		if !strings.Contains(string(text), "\\u0001") || !strings.Contains(string(text), "\\u001f") {
			t.Errorf("%s: U+0001 and U+001F must write as \\u0001 and \\u001f, got %q", name, text)
		}
		if strings.Contains(string(text), "\\u0000") {
			t.Errorf("%s: a control write spelled a zero byte: %q", name, text)
		}
		if !answer.read || answer.reread != answer.text {
			t.Errorf("%s: the control text did not read back unchanged (read=%v)", name, answer.read)
		}
	}

	for _, name := range []string{"field-clean", "list-clean", "key-clean"} {
		answer, seen := got[name]
		if !seen {
			t.Errorf("%s: the C++ probe reported no answer", name)
			continue
		}
		if answer.measure < 0 || answer.written != answer.measure {
			t.Errorf("%s: a value without a zero byte must write at its measured length, got measure=%d write=%d",
				name, answer.measure, answer.written)
			continue
		}
		text := decode(t, name, answer.text)
		if strings.Contains(string(text), "\\u0000") {
			t.Errorf("%s: a clean write spelled a zero byte: %q", name, text)
		}
		if !answer.read || answer.reread != answer.text {
			t.Errorf("%s: the written text did not read back unchanged (read=%v)", name, answer.read)
		}
	}
}
