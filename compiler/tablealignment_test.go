package compiler

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A variable wide root used to fail C++'s Alloc<T> assertion (16 <= 8).
// Every indirect closure path must influence the arena and framing oracle.
func TestCppVariableWideAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, source, text string
		align              int64
	}{
		{"narrow", "table Root { n uint64\nnext *Root }", `{"n":7,"next":{"n":9}}`, 8},
		{"direct", "table Root { n uint128\nnext *Root }", `{"n":1267650600228229401496703205383,"next":{"n":9}}`, 16},
		{"pointer", "fixed table Wide { n uint128 }\ntable Root { next *Wide }", `{"next":{"n":1267650600228229401496703205383}}`, 16},
		{"union", "union Choice { wide uint128 | overlay }\ntable Root { choice Choice\nnext *Root }", `{"choice":{"wide":1267650600228229401496703205383}}`, 16},
		{"list", "fixed table Wide { n uint128 }\ntable Root { values []Wide }", `{"values":[{"n":1267650600228229401496703205383},{"n":9}]}`, 16},
		{"map", "fixed table Wide { n uint128 }\ntable Root { values map[uint8]Wide }", `{"values":{"2":{"n":1267650600228229401496703205383}}}`, 16},
		{"array", "fixed table Wide { n uint128 }\ntable Root { values [2]Wide\nnext *Root }", `{"values":[{"n":1267650600228229401496703205383},{"n":9}]}`, 16},
		{"nested", "fixed table Wide { n uint128 }\ntable Root { value Wide\nnext *Root }", `{"value":{"n":1267650600228229401496703205383}}`, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, "package fixture\n"+tc.source+"\n")
			if got := ir.TableRegionAlign(u); got != tc.align {
				t.Fatalf("alignment %d, want %d", got, tc.align)
			}
			for _, lang := range []string{"cpp", "go"} {
				files, err := New().Generate(u, lang, Options{})
				if err != nil {
					t.Fatal(err)
				}
				needle := fmt.Sprintf("kTableAlign       = %d;", tc.align)
				suffix := "Table.h"
				if lang == "go" {
					needle = fmt.Sprintf("tableRegionAlign int64 = %d", tc.align)
					suffix = "Table.go"
				}
				found := false
				for name, content := range files {
					if strings.HasSuffix(name, suffix) && strings.Contains(string(content), needle) {
						found = true
					}
				}
				if lang == "cpp" {
					model := tabletext.NewModel(u)
					value := model.New(u.Tables["Root"])
					var report tabletext.Report
					if !model.Read(value, []byte(tc.text), &report) || !report.Silent() {
						t.Fatalf("text: %+v", report)
					}
					wire, err := tablewire.Encode(model, value)
					if err != nil {
						t.Fatal(err)
					}
					probe := fmt.Sprintf(`#include "ProbeTable.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace fixture;
#define REQUIRE(x) do { if (!(x)) { fprintf(stderr,"wide closure failed at line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
int main(){const uint8_t wire[]={%s}; RootBuilder builder; TableReport report;
REQUIRE(builder.GetRoot()!=nullptr); REQUIRE(uintptr_t(builder.GetRoot()) %% kTableAlign == 0);
REQUIRE(RootLoadBuilder(builder,wire,sizeof(wire),&report)); REQUIRE(!report.malformed);
auto size=RootMeasure(builder); REQUIRE(size==sizeof(wire)); auto output=(uint8_t*)malloc(size);
REQUIRE(RootSave(builder,output,size)==size); REQUIRE(memcmp(output,wire,size)==0);
REQUIRE(builder.Lock()); REQUIRE(uintptr_t(builder.AsConst()) %% kTableAlign == 0);
REQUIRE(RootMeasure(builder)==size); REQUIRE(RootSave(builder,output,size)==size); REQUIRE(memcmp(output,wire,size)==0);
auto regionBytes=RootLoadMeasure(wire,sizeof(wire)); REQUIRE(regionBytes>0);auto region=(uint8_t*)malloc(regionBytes);
REQUIRE(RootLoad(region,regionBytes,wire,sizeof(wire),&report)!=nullptr); REQUIRE(!report.malformed);
free(region); free(output); return 0;}
`, cppWire(wire))
					referenceCompileRun(t, files, probe)
				}
				if !found {
					t.Fatalf("%s arena missing %q", lang, needle)
				}
			}
		})
	}
}

// Compile the emitted table implementation with the actual serialize runtime.
// No replacement typedef can conceal the wide scalar alignment contract.
func referenceCompileRun(t *testing.T, files map[string][]byte, source string) {
	t.Helper()
	compiler, err := exec.LookPath("c++")
	if err != nil {
		t.Skipf("c++ unavailable: %v", err)
	}
	runtime, err := filepath.Abs("../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "serialize.h")); err != nil {
		t.Fatalf("real serialize runtime required: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, sanitize := range []bool{false, true} {
		t.Run(fmt.Sprintf("sanitizers=%v", sanitize), func(t *testing.T) {
			bin := filepath.Join(dir, "probe")
			args := []string{"-std=c++17", "-O1", "-g", "-Wall", "-Wextra", "-Werror", "-I" + runtime, path, "-o", bin}
			if sanitize {
				args = append(args, "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
			}
			if out, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			if out, err := exec.Command(bin).CombinedOutput(); err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
		})
	}
}

func cppWire(wire []byte) string {
	var result strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&result, "%d,", b)
	}
	return result.String()
}
