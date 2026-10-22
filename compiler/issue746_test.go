// THE INT32_MAX BEHAVIORAL REPRODUCTION #746 — the certify-tier twin of
// compiler/issue714_test.go's emitted-text pin.
//
// #714/#745 repaired the signed 32-bit overflow in the generated Base64
// writers (`i + 3 <= length` -> `length - i >= 3`), and issue714_test.go holds
// the emitted loop condition across all four targets. That pin is cheap by
// construction: it reads text and never runs the loop. This test is the
// behavioral half — it generates a C++ table that declares `bytes(N)` at the
// signed ceiling, compiles the generated JSON writer under Clang UBSan, and
// writes the whole 2.8 GiB Base64 body from an INT32_MAX input.
//
// On the pre-#745 emitter the loop condition evaluated `i + 3` when
// `i == 2147483646`, overflowing to a negative number that still compared
// `<= length`, so the loop ran past the end of the buffer. UBSan aborts on the
// first such evaluation (-fno-sanitize-recover=all), which is the red this test
// was written against; the repaired subtraction never wraps and the run
// terminates, emitting the measured length with no diagnostic.
//
// This is a CERTIFY-TIER test, not a per-commit one: a 2 GiB input plus a
// 2.8 GiB output costs over ten seconds and does not fit the owner's two-minute
// per-commit rule. It is therefore gated behind SCHEMA_CERTIFY_INT32_MAX, which
// `make tables-cpp-release` sets (and certify.yml runs nightly). Without the
// variable it skips, so `go test ./...` and `make test` stay cheap.
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const issue746Int32Max = 2147483647

// issue746Driver writes exactly INT32_MAX zero bytes through the generated
// writer, checks the measured length against an independent Base64 formula,
// and requires the write to hit that length. Nothing in it depends on the
// table's field count beyond the one bytes field.
const issue746Driver = `#include "ProbeTable.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>

using namespace certify;

int main()
{
    const int32_t n = 2147483647; // INT32_MAX
    Blob * value = new Blob();
    value->payload_length = n;
    const int64_t base64 = ( ( (int64_t) n + 2 ) / 3 ) * 4; // padded base64 body
    const int64_t need = BlobToJsonMeasure( *value );
    if ( need <= base64 ) { fprintf( stderr, "measured %lld is not past the base64 body %lld\n", (long long) need, (long long) base64 ); return 1; }
    char * out = (char *) malloc( (size_t) need + 1 );
    if ( out == NULL ) { fprintf( stderr, "output allocation failed\n" ); return 2; }
    const int64_t wrote = BlobToJson( *value, out, need );
    if ( wrote != need ) { fprintf( stderr, "wrote %lld, want the measured %lld\n", (long long) wrote, (long long) need ); return 3; }
    if ( out[0] != '{' ) { fprintf( stderr, "the text form is not an object\n" ); return 4; }
    printf( "int32_max base64: need=%lld base64=%lld\n", (long long) need, (long long) base64 );
    free( out );
    delete value;
    return 0;
}
`

func TestIssue746Base64WriterInt32MaxUBSan(t *testing.T) {
	if os.Getenv("SCHEMA_CERTIFY_INT32_MAX") != "1" {
		t.Skip("certify tier: set SCHEMA_CERTIFY_INT32_MAX=1 (2 GiB in, ~2.8 GiB out; `make tables-cpp-release` does)")
	}
	clang, err := exec.LookPath("clang++")
	if err != nil {
		t.Skip("the INT32_MAX Base64 reproduction is defined under Clang UBSan: no clang++ on PATH")
	}

	src := fmt.Sprintf("package certify\n\nfixed table Blob\n{\n    payload bytes(%d)\n}\n", issue746Int32Max)
	files, err := New().Generate(unitFromSource(t, src), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate cpp: %v", err)
	}
	if _, ok := files["ProbeTable.h"]; !ok {
		t.Fatal("the cpp target emitted no ProbeTable.h for the bytes(N) table")
	}
	if _, ok := files["ProbeTable.cpp"]; !ok {
		t.Fatal("the cpp target emitted no ProbeTable.cpp for the bytes(N) table")
	}

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(issue746Driver), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "int32max-base64")
	args := []string{
		"-std=c++17", "-O1", "-g", "-Wall", "-Wextra", "-Werror",
		"-fsanitize=undefined", "-fno-sanitize-recover=all",
		"-I", dir, main, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin,
	}
	if out, err := exec.Command(clang, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile under UBSan: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the INT32_MAX Base64 write did not terminate clean under UBSan: %v\n%s", err, out)
	}
	// The text form's measured length is a PIN, not a derived value: at
	// INT32_MAX the padded Base64 body is 2863311532 bytes and the object's
	// JSON framing around it is 20 more. A writer that wrapped would not reach
	// here at all; one that emitted the wrong length is caught here.
	line := strings.TrimSpace(string(out))
	if !strings.Contains(line, "need=2863311552 base64=2863311532") {
		t.Fatalf("unexpected emitted length: %q", line)
	}
	t.Logf("%s", line)
}
