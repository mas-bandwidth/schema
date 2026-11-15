package cstable

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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCSharpFixedNestedUnionNoneWidth crosses the C# compiled plan against the
// C++ fixed writer's bytes for a union nested in an outer union's arm when the
// two tags are stored at different widths.
//
// THE OUTER UNION IS TWO BYTES AND THE INNER ONE BYTE (SPEC-TABLES §3.4: a
// union tag is `ir.StorageBitsFor(Max)` bits). The C++ writer saves outer arm
// 256 with the inner union at None, and the C# reader compiles a plan from the
// C++ layout and runs it over those bytes. The reader's inner tag is first
// poisoned with an arm, the previous record's value, because the union's None
// const is what clears it when no arm matches (SPEC-TABLES §4.6, ALGORITHM
// §5.2 EMIT kind 15). The None const answers to the OUTER tag and must compare
// that tag at the OUTER width; a plan that stamps it with the inner width reads
// one byte of a two-byte tag and never fires.
func TestCSharpFixedNestedUnionNoneWidth(t *testing.T) {
	t.Parallel()
	src := nestedUnionNoneWidthSchema()
	body, layout := cppNestedUnionNoneWidthBytes(t, src)
	if len(body) < 3 || body[0] != 0x00 || body[1] != 0x01 || body[2] != 0x00 {
		t.Fatalf("the C++ writer did not save outer arm 256 with the inner union at None: % x", body)
	}
	csharpNestedUnionNoneWidthProbe(t, src, body, layout)
}

func nestedUnionNoneWidthSchema() string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Leaf { n int32 = 0 }\n\nunion Inner\n{\n    x Leaf\n    y Leaf\n}\n\nunion Outer\n{\n")
	for i := 1; i <= 255; i++ {
		fmt.Fprintf(&b, "    a%d Leaf\n", i)
	}
	b.WriteString("    nest Inner\n}\n\nfixed table Root\n{\n    o Outer\n}\n")
	return b.String()
}

// cppNestedUnionNoneWidthBytes generates the C++ fixed form, saves one record
// through the generated writer and answers its body and layout bytes. The
// probe prints each as hex; a missing C++ compiler is a skip, a failure under
// SCHEMA_REQUIRE_CORPUS.
func cppNestedUnionNoneWidthBytes(t *testing.T, src string) (body, layout []byte) {
	t.Helper()
	slowtest.Gate(t, "an external toolchain")

	u := unitFrom(t, src)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatalf("cpptable.Generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	header := ""
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = filepath.Base(name)
		}
	}
	if header == "" {
		t.Fatal("cpptable generated no Table.h")
	}
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "%s"

using namespace %s;

int main( void )
{
    Root v;
    RootReset( v );
    v.o.type = OuterType::Nest;
    v.o.nest.type = InnerType::None;
    uint8_t body[RootFixedBodyBytes];
    memset( body, 0, sizeof body );
    RootFixedWriteBody( body, v );
    printf( "BODY " );
    for ( int i = 0; i < (int) RootFixedBodyBytes; ++i ) { printf( "%%02x", body[i] ); }
    printf( "\nLAYOUT " );
    for ( int i = 0; i < (int) RootFixedLayoutBytes; ++i ) { printf( "%%02x", RootFixedLayout[i] ); }
    printf( "\n" );
    return 0;
}
`, header, u.Package)
	pp := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(pp, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command(cxx, "-std=c++17", "-O0", "-I"+dir, pp, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("the C++ probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ probe did not run: %v\n%s", err, out)
	}
	var gotBody, gotLayout string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "BODY "):
			gotBody = strings.TrimSpace(strings.TrimPrefix(line, "BODY "))
		case strings.HasPrefix(line, "LAYOUT "):
			gotLayout = strings.TrimSpace(strings.TrimPrefix(line, "LAYOUT "))
		}
	}
	if gotBody == "" || gotLayout == "" {
		t.Fatalf("the C++ probe printed no body or layout:\n%s", out)
	}
	body, err = hex.DecodeString(gotBody)
	if err != nil {
		t.Fatalf("body hex: %v", err)
	}
	layout, err = hex.DecodeString(gotLayout)
	if err != nil {
		t.Fatalf("layout hex: %v", err)
	}
	return body, layout
}

func csharpNestedUnionNoneWidthProbe(t *testing.T, src string, body, layout []byte) {
	t.Helper()
	bodyLit := hexByteArray(body)
	layoutLit := hexByteArray(layout)
	driver := fmt.Sprintf(`byte[] cppBody = new byte[] { %s };
byte[] cppLayout = new byte[] { %s };
if (!Probe.Schema.RootFixedLayout.AsSpan().SequenceEqual(cppLayout)) { bad += ProbeLog.Fail("the C# layout is not the C++ layout"); }
if (!Probe.Schema.TableFixedWire.ParseLayout(cppLayout, out Probe.TableFixedLayoutView parsed)) { bad += ProbeLog.Fail("the C++ layout does not parse"); }
Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[1 << 12];
int made = Probe.Schema.TableFixedWire.Compile(parsed, cppLayout, Probe.Schema.RootFixedDst, plan, new Probe.TableReport(), out int split);
if (made <= 0) { bad += ProbeLog.Fail("the C# compiler refused the C++ layout: " + made); }
Probe.Root back = new Probe.Root();
Probe.Schema.TableReset(back);
back.O.Type = (Probe.OuterType)256;
back.O.Nest.Type = Probe.InnerType.Y;
back.O.Nest.Y.N = 99;
byte[] planBytes = System.Runtime.InteropServices.MemoryMarshal.AsBytes<Probe.TableFixedEntry>(plan).ToArray();
byte[] scratch = Array.Empty<byte>();
int forged = 0;
Probe.TableReport r = new Probe.TableReport();
string why = Probe.Schema.RootFixedRun(new ReadOnlySpan<Probe.TableFixedEntry>(plan, 0, made), cppBody, back, r, planBytes, ref scratch, ref forged);
if (why != null) { bad += ProbeLog.Fail("the read refused the C++ bytes: " + why); }
if ((ulong)back.O.Type != 256) { bad += ProbeLog.Fail("the outer tag is " + (ulong)back.O.Type + ", want 256"); }
if ((ulong)back.O.Nest.Type != 0) { bad += ProbeLog.Fail("the inner tag is " + (ulong)back.O.Nest.Type + ", want None: the None const answered the outer tag at the wrong width"); }
`, bodyLit, layoutLit)
	t15RunCS(t, src, driver)
}

func hexByteArray(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}
