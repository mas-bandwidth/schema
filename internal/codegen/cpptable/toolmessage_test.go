package cpptable

// THE C++ REFERENCE READS EVERY messagedemo.ToolMessage INSTANCE THE CORPUS
// CARRIES (testdata/conformance/tables/MANIFEST.txt, the message class, §2.6).
// ToolMessage is a fixed root, so each file is form 3 (docs/SPEC-TABLES.md
// §3.4) and the conformance wire surface reads it through ToolMessageFixedLoad,
// the strict fixed-form read. The probe below is that read: each instance must
// read, save back to the file's own bytes, and write the same text the Go
// engine writes for the value it reads from the same file.
//
// The instances hold two layouts the dead-byte scan (§4.5) must judge by the
// arm that is taken: a nested union whose tag sits inside an outer arm that is
// not taken (the bytes there are that arm's live payload), and two arms of one
// union that begin at the same offset with different element widths.

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const cppToolMessageProbeMain = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
#include "MessagesTable.h"

using namespace messagedemo;

static bool spill( const char * path, const void * data, size_t bytes )
{
    FILE * f = std::fopen( path, "wb" );
    if ( f == NULL )
    {
        return false;
    }
    const bool ok = std::fwrite( data, 1, bytes, f ) == bytes;
    return std::fclose( f ) == 0 && ok;
}

int main( int argc, char ** argv )
{
    if ( argc < 4 )
    {
        std::printf( "usage: probe <file> <saved wire> <saved text>\n" );
        return 2;
    }
    static uint8_t data[1 << 20];
    FILE * f = std::fopen( argv[1], "rb" );
    if ( f == NULL )
    {
        std::printf( "the corpus file %s is missing\n", argv[1] );
        return 2;
    }
    const int64_t len = (int64_t) std::fread( data, 1, sizeof( data ), f );
    std::fclose( f );
    static ToolMessage value;
    static std::vector<TableFixedEntry> plan( 4096 );
    TableReport r;
    const int64_t n = ToolMessageFixedLoad( &value, 1, data, len, plan.data(), 4096, NULL, &r );
    if ( n != 1 || r.damaged || r.refused )
    {
        std::printf( "%s: n=%lld damaged=%d refused=%d reason=%d refuse_reason=%d, want one record read\n", argv[1],
                     (long long) n, (int) r.damaged, (int) r.refused, (int) (TableRefuseReason) r.reason, (int) r.refuse_reason );
        return 1;
    }
    std::vector<uint8_t> wire( (size_t) ToolMessageFixedMeasure( 1 ) );
    if ( ToolMessageFixedSave( &value, 1, wire.data(), (int64_t) wire.size() ) != (int64_t) wire.size() )
    {
        std::printf( "%s: ToolMessageFixedSave did not write ToolMessageFixedMeasure's answer\n", argv[1] );
        return 1;
    }
    const int64_t size = ToolMessageToJsonMeasure( value );
    if ( size < 0 )
    {
        std::printf( "%s: ToolMessageToJsonMeasure refused the value\n", argv[1] );
        return 1;
    }
    std::vector<char> text( (size_t) size + 1 );
    if ( ToolMessageToJson( value, text.data(), size ) != size )
    {
        std::printf( "%s: ToolMessageToJson did not write ToolMessageToJsonMeasure's answer\n", argv[1] );
        return 1;
    }
    if ( !spill( argv[2], wire.data(), wire.size() ) || !spill( argv[3], text.data(), (size_t) size ) )
    {
        std::printf( "%s: cannot write the probe's answers\n", argv[1] );
        return 2;
    }
    return 0;
}
`

// cppToolMessageInstances are the manifest's ToolMessage rows whose bodies
// reach past the root's first arm: message_ping, and the six that carry a
// nested union or an arm of a counted array.
var cppToolMessageInstances = []string{
	"message_ping",
	"message_open",
	"message_transaction",
	"message_batch",
	"message_trace",
	"message_arms",
	"message_arm_edits",
}

func TestCppReadsEveryToolMessageInstance(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated fixed-form read over the message corpus)")
	u := loadUnit(t, filepath.Join("..", "..", "..", "tables", "messages", "Messages.schema"))
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
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "MessagesTable.cpp")); err != nil {
		t.Fatalf("the generator wrote no MessagesTable.cpp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(cppToolMessageProbeMain), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, _ := filepath.Abs("../../../../serialize")
	probe := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize,
		filepath.Join(dir, "probe.cpp"), filepath.Join(dir, "MessagesTable.cpp"), "-o", probe}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}

	model := tabletext.NewModel(u)
	root := model.Lookup("ToolMessage")
	if root == nil {
		t.Fatal("messagedemo declares no ToolMessage")
	}
	corpus := filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "wire")
	for _, name := range cppToolMessageInstances {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, name+".bin")
			wire, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			value := model.New(root)
			var report tabletext.Report
			if ok, err := tablewire.Decode(model, value, wire, &report); !ok || err != nil {
				t.Fatalf("%s: the Go engine does not read the file: ok=%v err=%v", name, ok, err)
			}
			engineWire, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatalf("%s: the Go engine does not write the value back: %v", name, err)
			}
			engineText, err := model.Write(value)
			if err != nil {
				t.Fatalf("%s: the Go engine does not write the value's text: %v", name, err)
			}

			savedWire := filepath.Join(t.TempDir(), "saved.bin")
			savedText := filepath.Join(t.TempDir(), "saved.json")
			if out, err := exec.Command(probe, file, savedWire, savedText).CombinedOutput(); err != nil {
				t.Fatalf("%s: the C++ read: %v\n%s", name, err, out)
			}
			gotWire, err := os.ReadFile(savedWire)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotWire, engineWire) {
				t.Fatalf("%s: the C++ read saves %d bytes that differ from the Go engine's %d", name, len(gotWire), len(engineWire))
			}
			gotText, err := os.ReadFile(savedText)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimRight(gotText, "\n"), bytes.TrimRight(engineText, "\n")) {
				t.Fatalf("%s: the C++ value's text differs from the Go engine's:\nC++: %s\nGo:  %s", name, gotText, engineText)
			}
		})
	}
}
