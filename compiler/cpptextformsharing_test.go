package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The hand-text adapter must land the JSON graph walk's node identities,
// rather than copying a definition into every pointer slot (§16.7/§16.9.2).
// Run the same documents through independently generated C++ and Go readers.
func TestCPPTextFormSharing(t *testing.T) {
	slowtest.Gate(t, "the C++ and Go compilers")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Fatal(err)
	}
	schema := `package probe
 table GNode { id int32; next *GNode; inside Wrap }
 table Other { id int32 }
 table Wrap { target *GNode }
 union Choice { selected GNode }
 table Graph {
 items []*GNode
 edges map[string(8)]*GNode
 links [..3]*GNode
 fixedLinks [2]*GNode
 lookup map[string(8)]?int32
 blob *bytes
 direct *GNode
 other *Other
 nested Wrap
 values [..2]GNode
 choice Choice
 }
 `
	// Semicolons are not schema syntax.
	schema = strings.ReplaceAll(schema, ";", "\n")
	cases := []struct{ name, text, why string }{
		{"shared", `(:items (("&node" 7 :id 6) ("&node" 7) null))`, ""},
		{"tree", `(:items ((:id 6) (:id 6) null))`, ""},
		{"collection-sites", `(:items (("&node" 7 :id 6)) :edges (("same" ("&node" 7))) :links (("&node" 7) null) :fixedLinks (("&node" 7) null))`, ""},
		{"byvalue-descent", `(:items (("&node" 7 :id 6)) :values ((:next ("&node" 7))) :choice (:selected (:next ("&node" 7))))`, ""},
		{"nested", `(:items (("&node" 7 :id 6)) :nested (:target ("&node" 7)) :direct ("&node" 7))`, ""},
		{"completed-inner", `(:direct ("&node" 7 :id 1 :next ("&node" 9 :id 2)) :items (("&node" 9) ("&node" 7)))`, ""},
		{"map-key", `(:lookup (("&node" 2)))`, ""},
		{"optional-map-value", `(:lookup (("west" null) ("east" 2)))`, ""},
		{"empty-blob", `(:blob "")`, ""},
		{"forward", `(:items (("&node" 7) ("&node" 7 :id 6)))`, "text_reference"},
		{"self-cycle", `(:direct ("&node" 7 :id 6 :next ("&node" 7)))`, "text_reference"},
		{"byvalue-cycle", `(:direct ("&node" 7 :id 6 :inside (:target ("&node" 7))))`, "text_reference"},
		{"deep-cycle", `(:direct ("&node" 7 :id 6 :next (:next ("&node" 7))))`, "text_reference"},
		{"mismatch", `(:items (("&node" 7 :id 6)) :other ("&node" 7))`, "text_reference"},
		{"duplicate", `(:items (("&node" 7 :id 6) ("&node" 7 :id 8)))`, "text_reference"},
		{"empty-definition", `(:items (("&node" 7)))`, "text_reference"},
		{"after-field", `(:direct (:id 6 "&node" 7))`, "text_reference"},
		{"root", `("&node" 7 :items ())`, "text_reference"},
		{"byvalue", `(:nested ("&node" 7 :target null))`, "text_reference"},
		{"byvalue-element", `(:values (("&node" 7 :id 6)))`, "text_reference"},
		{"union-arm", `(:choice (:selected ("&node" 7 :id 6)))`, "text_reference"},
		{"unknown-prefix", `(:direct ("&other" 7 :id 6))`, "text_reference"},
	}
	for _, label := range []string{"0", "-1", "1.0", "1e0", `"7"`, "null"} {
		cases = append(cases, struct{ name, text, why string }{"label-" + label, `(:direct ("&node" ` + label + ` :id 6))`, "text_reference"})
	}
	// The fixture includes a present empty *bytes and ordered optional map values.
	cases = append(cases, struct{ name, text, why string }{"variable-fixture", string(cppMustRead(t, "variable.input.sexp")), ""})
	dirs := map[string]string{}
	for _, lang := range []string{"cpp", "go"} {
		dir := t.TempDir()
		dirs[lang] = dir
		opts := Options{}
		if lang == "go" {
			opts = Options{"runtime": "standalone", "surface": "textform"}
		}
		files, err := New().Generate(unitFromSource(t, schema), lang, opts)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	cppdir := dirs["cpp"]
	if err := os.WriteFile(filepath.Join(cppdir, "main.cpp"), []byte(cppTextFormSharingDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppdir, "probe")
	if out, err := exec.Command(cxx, "-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", cppdir, filepath.Join(cppdir, "main.cpp"), filepath.Join(cppdir, "ProbeTable.cpp"), "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	var vectors strings.Builder
	for _, tc := range cases {
		fmt.Fprintf(&vectors, "{%q,%q},\n", tc.text, tc.why)
	}
	goSource := strings.ReplaceAll(goTextFormSharingDriver, "VECTORS", vectors.String())
	godir := dirs["go"]
	runtime := slowtest.SerializeGo(t, "../")
	mod := fmt.Sprintf("module probe\n\ngo 1.26\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	for name, body := range map[string]string{"go.mod": mod, "sharing_test.go": goSource} {
		if err := os.WriteFile(filepath.Join(godir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = godir
	cmd.Env = append(os.Environ(), "SHARING_RESULTS="+godir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go oracle: %v\n%s", err, out)
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command(bin, tc.text).CombinedOutput()
			if err != nil {
				t.Fatalf("probe: %v\n%s", err, out)
			}
			if tc.why != "" {
				if strings.TrimSpace(string(out)) != tc.why {
					t.Fatalf("got %s, want %s", out, tc.why)
				}
				return
			}
			oracle, err := os.ReadFile(filepath.Join(godir, fmt.Sprintf("case-%d.json", i)))
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("C++ JSON %q: %v", out, err)
			}
			if err := json.Unmarshal(oracle, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("C++ %s\nGo %s", out, oracle)
			}
		})
	}
	// JSON's tolerant map walk retains optional presence for a non-null
	// wrong-kind value, just as the ordinary optional-field walk does.
	wrongShape, err := exec.Command(bin, "json-kind", `{"lookup":{"east":false}}`).CombinedOutput()
	var refused map[string]any
	if err != nil || json.Unmarshal(wrongShape, &refused) != nil {
		t.Fatalf("optional map JSON refusal: %v\n%s", err, wrongShape)
	}
	if got := refused["lookup"].(map[string]any)["east"]; got != float64(0) {
		t.Fatalf("wrong-kind optional map value lost presence: %v", got)
	}
	// Source labels are identities, not storage indices: a positive spelling
	// beyond uint64 still maps to the same internal node.
	large := `(:items (("&node" 184467440737095516160000000 :id 6) ("&node" 184467440737095516160000000)))`
	out, err := exec.Command(bin, large).CombinedOutput()
	if err != nil || !strings.Contains(string(out), `"&node"`) {
		t.Fatalf("large source label: %v\n%s", err, out)
	}
	// Editing the defining node must update both slots, retaining every other byte.
	input := "; shared node\n(:items ((\"&node\" 7 :id 6) ; reference\n (\"&node\" 7) null))\n"
	out, err = exec.Command(bin, input, "items[0].id", "8").CombinedOutput()
	if err != nil {
		t.Fatalf("edit: %v\n%s", err, out)
	}
	if string(out) != strings.Replace(input, ":id 6", ":id 8", 1) {
		t.Fatalf("edit changed unrelated bytes: %q", out)
	}
	out, err = exec.Command(bin, input, "items[0].id", "6").CombinedOutput()
	if err != nil || string(out) != input {
		t.Fatalf("no-op edit: %v\n%s", err, out)
	}
	out, err = exec.Command(bin, input, "items[1].id", "8").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "text_edit_conflict" {
		t.Fatalf("reference has no editable field token: %v\n%s", err, out)
	}
}

const cppTextFormSharingDriver = `#include <cstdio>
#include <cstring>
#include <vector>
#include "ProbeTable.h"
using namespace probe;
int main(int argc,char **argv) {
 if(argc<2)return 2;
 const char *doc=NULL,*why=NULL;int64_t bytes=0;GraphBuilder b;
 if(argc==3 && strcmp(argv[1],"json-kind")==0){
  TableReport report;
  if(!GraphFromJson(b,argv[2],(int64_t)strlen(argv[2]),&report) || !report.refused || report.refuse_reason!=kind_mismatch || !b.Lock())return 6;
  const Graph *g=b.AsConst();std::vector<char> out((size_t)GraphToJsonMeasure(g)+1);
  int64_t n=GraphToJson(g,out.data(),(int64_t)out.size());if(n<0)return 4;
  fwrite(out.data(),1,(size_t)n,stdout);return 0;
 }
 if(!GraphLoadTextForm(argv[1],(int64_t)strlen(argv[1]),&doc,&bytes,b,&why)){printf("%s",why?why:"?");return 0;}
 std::vector<char> saved((size_t)bytes+1);
 if(GraphSaveTextForm(doc,bytes,saved.data(),(int64_t)saved.size())!=bytes || memcmp(saved.data(),argv[1],(size_t)bytes)!=0)return 5;
 if(argc==4){
  TableTextFormEdit e;e.path=argv[2];e.text=argv[3];std::vector<char> out(1<<20);int64_t n=0;
  if(!GraphEditTextForm(doc,bytes,&e,1,out.data(),(int64_t)out.size(),&n,b,&why)){printf("%s",why?why:"?");return 0;}
  if(!b.Lock())return 3;
  const Graph *g=b.AsConst();
  if(g->items.size()!=3 || g->items[0]!=g->items[1] || g->items[2]!=NULL || g->items[0]->id!=(strcmp(argv[3],"8")==0?8:6))return 4;
  fwrite(out.data(),1,(size_t)n,stdout);return 0;
 }
 if(!b.Lock())return 3;
 const Graph *g=b.AsConst();
 std::vector<char> out((size_t)GraphToJsonMeasure(g)+1);
 int64_t n=GraphToJson(g,out.data(),(int64_t)out.size());if(n<0)return 4;
 fwrite(out.data(),1,(size_t)n,stdout);return 0;
}`

const goTextFormSharingDriver = `package probe
import("bytes";"fmt";"os";"path/filepath";"testing")
func TestSharing(t *testing.T){
 cases:=[]struct{text,why string}{VECTORS}
 for i,tc:=range cases {
  doc,v,err:=GraphLoadTextForm([]byte(tc.text),TableTextFormDefaultLimits())
  if tc.why!="" { if err==nil || err.(*TableTextFormError).Name!=tc.why {t.Fatalf("case %d: %v, want %s",i,err,tc.why)};continue }
  if err!=nil{t.Fatalf("case %d: %v",i,err)}
  original,err:=GraphSaveTextForm(doc);if err!=nil || !bytes.Equal(original,[]byte(tc.text)){t.Fatalf("rewrite %d: %v",i,err)}
  out:=make([]byte,GraphToJSONMeasure(v));if GraphToJSON(v,out)!=int64(len(out)){t.Fatalf("json %d",i)}
  if err:=os.WriteFile(filepath.Join(os.Getenv("SHARING_RESULTS"),fmt.Sprintf("case-%d.json",i)),out,0600);err!=nil{t.Fatal(err)}
 }
}
`
