package cpptable

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10; docs/rulings.json
// fixed-dead-bytes-are-zero): an absent optional's payload, a counted array's
// element slots past its count, a string's or bytes' bytes past its used
// length, and an inactive union arm. The shared corpus (fixedvector.DeadBytes)
// carries a control with every dead byte zero and one forged file per dead
// region. The reference's own reader refuses the forged files whole, before a
// record lands, and reads the controls; each control saves back byte for byte.
//
// The verdict is the contract; a refusal's name is the language's own
// diagnostic and is never compared here.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppFixedDeadBytesProbeMain = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
#include "@HEADER@"

using namespace @PKG@;

int main( int argc, char ** argv )
{
    if ( argc < 3 )
    {
        std::printf( "usage: probe <file> <refuse|read>\n" );
        return 2;
    }
    const bool want_refuse = std::strcmp( argv[2], "refuse" ) == 0;
    static uint8_t data[1 << 20];
    FILE * f = std::fopen( argv[1], "rb" );
    if ( f == NULL ) { std::printf( "the corpus file %s is missing\n", argv[1] ); return 2; }
    const int64_t len = (int64_t) std::fread( data, 1, sizeof( data ), f );
    std::fclose( f );
    if ( len < 20 ) { std::printf( "%s is shorter than a form-3 header\n", argv[1] ); return 2; }

    DeadRoot storage[1];
    std::memset( (void *) storage, 0xA5, sizeof( storage ) ); // a destination the load must not write
    DeadRoot before[1];
    std::memcpy( (void *) before, (const void *) storage, sizeof( storage ) );
    std::vector<TableFixedEntry> plan( 4096 );
    TableReport r;
    const int64_t n = DeadRootFixedLoad( storage, 1, data, len, plan.data(), 4096, NULL, &r );
    if ( want_refuse )
    {
        if ( n != -1 )
        {
            std::printf( "%s: read %lld records, want the whole load refused\n", argv[1], (long long) n );
            return 1;
        }
        if ( std::memcmp( (const void *) storage, (const void *) before, sizeof( storage ) ) != 0 )
        {
            std::printf( "%s: a refused load wrote the caller's destination\n", argv[1] );
            return 1;
        }
        return 0;
    }
    if ( n != 1 || r.damaged || r.refused )
    {
        std::printf( "%s: n=%lld damaged=%d refused=%d reason=%d refuse_reason=%d, want one record read\n", argv[1],
                     (long long) n, (int) r.damaged, (int) r.refused, (int) r.reason, (int) r.refuse_reason );
        return 1;
    }
    std::vector<uint8_t> out( (size_t) DeadRootFixedMeasure( 1 ) );
    const int64_t wrote = DeadRootFixedSave( storage, 1, out.data(), (int64_t) out.size() );
    if ( wrote != (int64_t) out.size() || wrote != len || std::memcmp( out.data(), data, (size_t) len ) != 0 )
    {
        std::printf( "%s: the read did not save back byte for byte (wrote=%lld len=%lld)\n",
                     argv[1], (long long) wrote, (long long) len );
        return 1;
    }
    return 0;
}
`

// cppFixedDeadBytesProbe generates the C++ fixed form from the dead-byte
// schema, compiles the probe once, and returns the binary. Each vector runs
// the same binary, so the corpus is read by the exact reader the schema
// produces and the compile is paid once.
func cppFixedDeadBytesProbe(t *testing.T) string {
	slowtest.Gate(t, "an external toolchain")
	t.Helper()
	schema := filepath.Join("..", "..", "..", "test", "tables", "DeadBytes.schema")
	u := loadUnit(t, schema)
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
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := strings.ReplaceAll(strings.ReplaceAll(cppFixedDeadBytesProbeMain, "@HEADER@", header), "@PKG@", u.Package)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, _ := filepath.Abs("../../../../serialize")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	return filepath.Join(dir, "probe")
}

func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated reader over the corpus)")
	corpus := cppFixedCorpus(t)
	probe := cppFixedDeadBytesProbe(t)
	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v and DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			verdict := "refuse"
			if !want.Refused {
				verdict = "read"
			}
			out, err := exec.Command(probe, filepath.Join(corpus, vec.File), verdict).CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
