package cpptable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func cppMessageFlagsDomainRun(t *testing.T, plant bool) (string, error) {
	t.Helper()
	var names []string
	for i := range 64 {
		names = append(names, fmt.Sprintf("F%d", i))
	}
	u := cppUnitFrom(t, `package probe
 flags Features { One, Two }
 flags Full { `+strings.Join(names, ", ")+` }
 enum Key { First, Second }
 union Choice { mask Features }
 fixed table Node { sample Features }
 fixed table Root {
  direct Features
  specified Features = { One }
  child Node
  samples [2]Features
  counted [..2]Features
  keyed [Key]Features
  choice Choice
  optional ?Features
 }
 fixed table Wide { full Full }
 `)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(v, []byte(`{"direct":["One"],"specified":["Two"],"child":{"sample":["Two"]},"samples":[["One"],["One","Two"]],"counted":[["One"]],"keyed":{"First":["Two"]},"choice":{"mask":["One"]},"optional":["Two"]}`), &r) {
		t.Fatalf("valid engine value: %+v", r)
	}
	wire, err := tablewire.EncodeMessage(m, v)
	if err != nil {
		t.Fatal(err)
	}
	bad := m.New(m.Lookup("Root"))
	bad.Fields[0].Cell.U = 1 << 40
	if b, e := tablewire.EncodeMessage(m, bad); e == nil || b != nil {
		t.Fatal("engine message writer accepted unknown flags")
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if plant {
		hits := 0
		for name, data := range files {
			var out strings.Builder
			for _, line := range strings.SplitAfter(string(data), "\n") {
				if strings.Contains(line, "// message flags domain, debug only") {
					hits++
					continue
				}
				out.WriteString(line)
			}
			files[name] = []byte(out.String())
		}
		if hits == 0 {
			t.Fatal("flags assertion omission matched nothing")
		}
	}
	dir := t.TempDir()
	var sources []string
	header := ""
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	var literal strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&literal, "0x%02x,", b)
	}
	source := strings.ReplaceAll(cppMessageFlagsDomainMain, "ORACLE_BYTES", literal.String())
	source = strings.ReplaceAll(source, "TABLE_HEADER", header)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	var output bytes.Buffer
	for _, profile := range []struct {
		name  string
		flags []string
	}{{"debug", []string{"-O0"}}, {"optimized-debug", []string{"-O2"}}, {"release", []string{"-O2", "-DNDEBUG"}}} {
		bin := filepath.Join(dir, profile.name)
		args := []string{"-std=c++17", "-I" + dir}
		args = append(args, profile.flags...)
		args = append(args, filepath.Join(dir, "probe.cpp"))
		args = append(args, sources...)
		args = append(args, "-o", bin)
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			return string(out), err
		}
		out, err := exec.Command(bin).CombinedOutput()
		fmt.Fprintf(&output, "%s: %s", profile.name, out)
		if err != nil {
			return output.String(), err
		}
	}
	return output.String(), nil
}

const cppMessageFlagsDomainMain = `#include "TABLE_HEADER"
#include <cstdio>
#include <cstring>
#include <csignal>
#include <initializer_list>
#include <sys/wait.h>
#include <sys/resource.h>
#include <unistd.h>
using namespace probe;
int main(){
 struct rlimit no_core={0,0};setrlimit(RLIMIT_CORE,&no_core);
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary vocab(entries,kTableMessageEntriesHere);
 uint8_t announce[32768];int64_t an=Announce(announce,sizeof(announce));TableReport report;
 if(an<1||!AnnounceRead(vocab,announce,an,&report)){puts("announcement failed");return 1;}
 const uint8_t oracle[]={ORACLE_BYTES};Root value;int64_t count=1;
 if(!RootLoadMessages(&value,&count,vocab,oracle,sizeof(oracle),&report)||count!=1){puts("legal Go message refused");return 1;}
 uint8_t save[1024];int64_t n=RootSaveMessages(&value,1,save,sizeof(save),&report);
 if(n!=(int64_t)sizeof(oracle)||memcmp(save,oracle,sizeof(oracle))){puts("legal Go bytes changed");return 1;}
 Wide wide;WideReset(wide);wide.full=UINT64_C(1)<<63;
 n=WideSaveMessages(&wide,1,save,sizeof(save),&report);Wide back;count=1;
 if(n<1||!WideLoadMessages(&back,&count,vocab,save,n,&report)||back.full!=wide.full){puts("full-width64 mask lost");return 1;}
#ifndef NDEBUG
 const char*names[]={"direct","specified","nested","fixed-array","counted-array","keyed","union","optional"};
 for(int site=0;site<8;site++)for(int bit: {2,40,63})for(int api=0;api<2;api++){
  if(site!=0&&bit!=40)continue;
  pid_t pid=fork();if(pid<0){puts("fork failed");return 1;}if(pid==0){
   freopen("/dev/null","w",stderr);alarm(3);Root w;RootReset(w);const uint64_t high=(UINT64_C(1)<<bit)|1;
   switch(site){case 0:w.direct=high;break;case 1:w.specified=high;break;case 2:w.child.sample=high;break;
    case 3:w.samples[0]=high;break;case 4:w.counted_count=1;w.counted[0]=high;break;case 5:w.keyed[Key::First]=high;break;
    case 6:w.choice.type=ChoiceType::Mask;w.choice.mask=high;break;case 7:w.optional_present=true;w.optional=high;break;}
   TableReport r;if(api==0)(void)RootMeasureMessages(&w,1,&r);else{uint8_t b[1024];(void)RootSaveMessages(&w,1,b,sizeof(b),&r);}_exit(0);
  }
  int status=0;if(waitpid(pid,&status,0)!=pid||!WIFSIGNALED(status)||WTERMSIG(status)!=SIGABRT){printf("MISSING FLAGS ASSERT %s %s bit%d\n",names[site],api?"save":"measure",bit);return 1;}
 }
#endif
 puts("legal flags and full-width64 held; debug misuse assertions held");return 0;
}
`

func TestCppMessageFlagsWriteDomain(t *testing.T) {
	slowtest.Gate(t, "C++ compiler: message flags writer domain")
	if out, err := cppMessageFlagsDomainRun(t, false); err != nil {
		t.Fatalf("message flags domain: %v\n%s", err, out)
	} else {
		t.Log(out)
	}
}
func TestCppMessageFlagsWriteDomainNegativeControl(t *testing.T) {
	slowtest.Gate(t, "C++ compiler: flags writer assertion omission")
	out, err := cppMessageFlagsDomainRun(t, true)
	if err == nil || !strings.Contains(out, "MISSING FLAGS ASSERT direct measure") {
		t.Fatalf("assert omission did not fail named witness: %v\n%s", err, out)
	}
}
