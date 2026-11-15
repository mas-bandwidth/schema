package cpptable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFunctionalCppLongPointerChain holds the C++ leg to docs/SPEC-TABLES.md
// §3.1: a chain is not a depth, so a legal file carrying a 200,000-node chain
// loads through the leg's strict read on the leg's default stack, saves to the
// engine's canonical bytes for that value, writes its text (the writer's depth
// cap refuses a chain past it), and releases the value, all in time linear in
// the node count. The manifest's node_chain_two is the small control.
func TestFunctionalCppLongPointerChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	u := loadHeldUnitPaths(t, root, []string{filepath.Join("tables", "pointers")})
	raw, err := os.ReadFile(filepath.Join(framing, "node_chain_200000.bin"))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Scene"))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, inst, raw, &report)
	if err != nil || !ok {
		t.Fatalf("the engine refused node_chain_200000: ok=%t err=%v report=%+v", ok, err, report)
	}
	canonical, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath := filepath.Join(t.TempDir(), "node_chain_200000_canonical.bin")
	if err := os.WriteFile(canonicalPath, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
    std::vector<uint8_t> control, chain, canonical;
    if ( !slurp( %q, control ) ) { printf( "node_chain_two: the file is missing\n" ); return 1; }
    if ( !slurp( %q, chain ) ) { printf( "node_chain_200000: the file is missing\n" ); return 1; }
    if ( !slurp( %q, canonical ) ) { printf( "node_chain_200000: the canonical file is missing\n" ); return 1; }

    {
        TableRefuseReason why = ok;
        const ListNode * value = ListNodeReadStrict( control.data(), (int64_t) control.size(), &why );
        if ( value == NULL ) { printf( "node_chain_two: the strict load refused: reason=%%d\n", (int) why ); return 1; }
        const int64_t control_need = ListNodeMeasure( value );
        std::vector<uint8_t> control_saved( (size_t) ( control_need > 0 ? control_need : 64 ) );
        if ( ListNodeSave( value, control_saved.data(), (int64_t) control_saved.size() ) != control_need )
            { printf( "node_chain_two: save did not write its measure\n" ); return 1; }
        table_release( TableDefaultAllocator(), (void *) value );
    }

    TableRefuseReason why = ok;
    const Scene * value = SceneReadStrict( chain.data(), (int64_t) chain.size(), &why );
    if ( value == NULL ) { printf( "node_chain_200000: the strict load refused: reason=%%d\n", (int) why ); return 1; }

    const int64_t need = SceneMeasure( value );
    if ( need != (int64_t) canonical.size() ) { printf( "node_chain_200000: measure=%%lld canonical=%%zu\n", (long long) need, canonical.size() ); return 1; }
    std::vector<uint8_t> saved( (size_t) need );
    if ( SceneSave( value, saved.data(), (int64_t) saved.size() ) != need || memcmp( saved.data(), canonical.data(), canonical.size() ) != 0 )
        { printf( "node_chain_200000: save differs from the canonical bytes\n" ); return 1; }

    if ( SceneToJsonMeasure( value ) >= 0 )
        { printf( "node_chain_200000: the text writer accepted a chain past its depth cap\n" ); return 1; }

    table_release( TableDefaultAllocator(), (void *) value );
`, filepath.Join(framing, "node_chain_two.bin"), filepath.Join(framing, "node_chain_200000.bin"), canonicalPath)
	out, err := cppLongChainRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// cppLongChainRun is cppHeldRun under the functional test's 120 s wall bound.
func cppLongChainRun(t *testing.T, u *ir.Unit, body string) ([]byte, error) {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	outDir := t.TempDir()
	var headers, sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(outDir, name))
		}
	}
	if len(headers) == 0 {
		t.Fatal("no Table.h was generated")
	}
	var includes strings.Builder
	for _, h := range headers {
		fmt.Fprintf(&includes, "#include \"%s\"\n", h)
	}
	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
%s
using namespace %s;

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    fseek( f, 0, SEEK_END );
    const long n = ftell( f );
    fseek( f, 0, SEEK_SET );
    out.resize( (size_t) n );
    if ( n > 0 && fread( out.data(), 1, (size_t) n, f ) != (size_t) n ) { fclose( f ); return false; }
    fclose( f );
    return true;
}

int main( void )
{
%s
    return 0;
}
`, includes.String(), u.Package, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	native, err := filepath.Abs(filepath.Join("..", "..", "..", "test", "tables"))
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "serialize"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + native, "-I" + serialize, filepath.Join(outDir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(outDir, "probe"))
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, filepath.Join(outDir, "probe")).CombinedOutput()
}
