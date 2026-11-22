package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelMessageStrictMode holds the OPT-IN STRICT MODE of the
// announcement read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to
// the Go reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with v.Strict unset an endpoint of another build version reads clean; with
// it set the unit's own version reads clean and another is refused
// `build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions (report.remote, report.own), never damage,
// and terminal: the next announcement is `second_announcement` and a batch is
// `no_vocabulary`.
func TestSquirrelMessageStrictMode(t *testing.T) {
	t.Parallel()
	if got := squirrelMessageStrictMode(t, nil); got != "ok" {
		t.Fatalf("strict probe: %s", got)
	}
}

// TestSquirrelMessageStrictModeNegativeControl plants the old reader back (no
// strict check) in schema_table_message.nut and requires the probe to go red.
func TestSquirrelMessageStrictModeNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = `  if(("Strict" in v)&&v.Strict){` + "\n"
	got := squirrelMessageStrictMode(t, func(src []byte) []byte {
		if strings.Count(string(src), anchor) != 1 {
			t.Fatalf("schema_table_message.nut carries the strict check %d times, want 1", strings.Count(string(src), anchor))
		}
		return []byte(strings.Replace(string(src), anchor, "  if(false){\n", 1))
	})
	if got == "ok" {
		t.Fatal("the reader without the strict check still passes the strict probe")
	}
}

const squirrelStrictModeProbe = `local T=dofile("schema_module.nut",true).require(%q);
local own=0, remote=0, failures=[];
for(local i=0;i<8;i++)own=own|((T.TableMessageAnnouncement[3+i]&0xff)<<(8*i));
remote=~own;
local check=function(ok,what){if(!ok)failures.append(what);};
// the unit's own announcement with its build version word replaced: byte 0 is
// the form, byte 1 the version's reference, byte 2 its kind (9)
local endpoint=function(version){
  local b=blob(T.TableMessageAnnouncement.len());
  for(local i=0;i<b.len();i++)b[i]=T.TableMessageAnnouncement[i];
  for(local i=0;i<8;i++)b[3+i]=(version>>(8*i))&0xff;
  return b;
};
if((T.TableMessageAnnouncement[2]&0xff)!=9)failures.append("the announcement does not open with the build version");
// default_tolerant
local v={}, r={};
check(T.AnnounceRead(v,endpoint(remote),r),"the tolerant read refused another build version");
check(("Announced" in v)&&v.Announced&&v.BuildVersion==remote&&r.reason==null,"tolerant read");
// strict_same_version
v={Strict=true};r={};
check(T.AnnounceRead(v,endpoint(own),r),"the strict read refused its own build version");
check(("Announced" in v)&&v.Announced&&r.reason==null&&!("remote" in r),"strict same-version read");
// strict_other_version, typed refusal
v={Strict=true};r={};
check(!T.AnnounceRead(v,endpoint(remote),r),"the strict read accepted another build version");
check(r.reason=="build_version_mismatch","reason: "+r.reason);
check(!r.bodyDamaged,"the strict refusal is damage");
check(("remote" in r)&&r.remote==remote&&("own" in r)&&r.own==own,"the refusal does not carry both versions");
check(!(("Announced" in v)&&v.Announced)&&!("Count" in v),"the strict refusal set a vocabulary");
check(("BuildVersion" in v)&&v.BuildVersion==remote,"the peer's build version was not kept");
check(r.unknown==0,"the strict refusal moved a counter");
// terminal
local second={};
check(T.AnnounceRead(v,endpoint(own),second)!=true,"an announcement after a strict refusal was read");
check(second.reason=="second_announcement","after a strict refusal: "+second.reason);
local body={};
local count=T.RootLoadMessages([null],v,blob(2),body);
check(count==0&&body.reason=="no_vocabulary","a batch on the refused connection: "+body.reason);
print(failures.len()==0?"ok":failures.reduce(@(a,b) a+"; "+b));
`

// squirrelMessageStrictMode runs the probe, schema_table_message.nut edited
// by patch when set, and answers its one line.
func squirrelMessageStrictMode(t *testing.T, patch func([]byte) []byte) string {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	writeFile(t, schemaPath, []byte("package probe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n"))
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	var replace map[string][]byte
	if patch != nil {
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src, ok := tables["schema_table_message.nut"]
		if !ok {
			t.Fatal("no schema_table_message.nut in the generated tables")
		}
		replace = map[string][]byte{"schema_table_message.nut": patch(src)}
	}
	out := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, replace) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(out, name), b)
		}
	}
	writeFile(t, filepath.Join(out, "main.nut"), fmt.Appendf(nil, squirrelStrictModeProbe, squirreltable.ModuleName(u)))
	return strings.TrimSpace(string(sqrun.Run(t, out, "main.nut", nil)))
}
