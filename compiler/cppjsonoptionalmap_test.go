package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// A map's absent optional value must remain in the map as null (§2.8/§16.2).
// Reading a non-null value supplies presence, including zero; accepting null
// must preserve the public JSON API's wrong-kind refusal and root reset.
func TestCPPJSONOptionalMapValues(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Fatal(err)
	}
	const schema = `package probe
 table Graph { lookup map[string(8)]?int32 }
 `
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(driver, []byte(cppJSONOptionalMapDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	if out, err := testproc.Command(t, cxx, "-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, driver, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	cases := []struct {
		name, mode, input, reason string
		want                      map[string]any
	}{
		{"empty", "json", `{"lookup":{}}`, "ok", map[string]any{}},
		{"null", "json", `{"lookup":{"west":null}}`, "ok", map[string]any{"west": nil}},
		{"value", "json", `{"lookup":{"east":2}}`, "ok", map[string]any{"east": float64(2)}},
		{"present-zero", "json", `{"lookup":{"east":0}}`, "ok", map[string]any{"east": float64(0)}},
		{"mixed", "json", `{"lookup":{"west":null,"east":2}}`, "ok", map[string]any{"west": nil, "east": float64(2)}},
		{"wrong-kind", "json", `{"lookup":{"east":false}}`, "kind_mismatch", map[string]any{}},
		{"wrong-kind-after-null", "json", `{"lookup":{"west":null,"east":false}}`, "kind_mismatch", map[string]any{}},
		{"wrong-kind-after-value", "json", `{"lookup":{"west":2,"east":false}}`, "kind_mismatch", map[string]any{}},
		{"handtext", "text", "; optional map\n(:lookup ((\"west\" null) (\"east\" 2)))\n", "ok", map[string]any{"west": nil, "east": float64(2)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := testproc.Command(t, bin, tc.mode, tc.input).CombinedOutput()
			if err != nil {
				t.Fatalf("probe: %v\n%s", err, out)
			}
			reason, body, ok := bytes.Cut(out, []byte{'\n'})
			if !ok || string(reason) != tc.reason {
				t.Fatalf("got %q, want %s then JSON", out, tc.reason)
			}
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode %q: %v", body, err)
			}
			if !reflect.DeepEqual(got["lookup"], tc.want) {
				t.Fatalf("got %s, want lookup %+v", body, tc.want)
			}
		})
	}
}

const cppJSONOptionalMapDriver = `#include <cstdio>
#include <cstring>
#include <vector>
#include "ProbeTable.h"
using namespace probe;
int main(int argc,char **argv)
{
 if(argc!=3)return 2;
 GraphBuilder builder;
 const int64_t bytes=(int64_t)strlen(argv[2]);
 if(strcmp(argv[1],"json")==0)
 {
  TableReport report;
  const bool ok=GraphFromJson(builder,argv[2],bytes,&report);
  if(ok)
  {
   if(report.refused||report.damaged||report.unknown!=0)return 3;
   puts("ok");
  }
  else
  {
   if(!report.refused||report.damaged||report.refuse_reason!=kind_mismatch)return 4;
   puts("kind_mismatch");
  }
 }
 else
 {
  const char *document=NULL,*why=NULL;int64_t document_bytes=0;
  if(!GraphLoadTextForm(argv[2],bytes,&document,&document_bytes,builder,&why))return 5;
  std::vector<char> saved((size_t)bytes+1);
  if(GraphSaveTextForm(document,document_bytes,saved.data(),(int64_t)saved.size())!=bytes || memcmp(saved.data(),argv[2],(size_t)bytes)!=0)return 6;
  puts("ok");
 }
 if(!builder.Lock())return 7;
 const Graph *graph=builder.AsConst();
 std::vector<char> out((size_t)GraphToJsonMeasure(graph)+1);
 const int64_t n=GraphToJson(graph,out.data(),(int64_t)out.size());if(n<0)return 8;
 fwrite(out.data(),1,(size_t)n,stdout);
 return 0;
}
`
