package darttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// SPEC §6.6: explicit fixed-class roots have named refusals, including in
// all-fixed units. An ordinary table with only scalar fields is still variable.
func TestDartFixedRetainNamedRefusal(t *testing.T) {
	slowtest.Gate(t, "dart")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_%v", mixed), func(t *testing.T) {
			schema := "package probe\nfixed table Fixed { value int32 }\n"
			if mixed {
				schema += "table Variable { value int32 }\n"
			}
			u := unitFrom(t, schema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			source := `import 'ProbeWire.dart' as w;
import 'ProbeFixed.dart' as f;
void check(bool ok,String name){if(!ok)throw StateError(name);}
void main(){
 for(final reason in <String>[w.fixedWireLoadRetain,w.fixedWireMeasureRetain,w.fixedWireSaveRetain,f.fixedLoadRetainMessages,f.fixedSaveRetainMessages]){
  check(reason.contains('Fixed')&&reason.contains('variable root'),'named fixed retaining refusal');
 }
 final value=w.Fixed()..value=37;
 final report=w.TableWireReport();
 check(w.fixedWireLoad(w.fixedWireSave(value),report).value==37&&!report.malformed&&!report.refused,'fixed plain wire remains supported');
`
			if mixed {
				source += ` final value2=w.Variable()..value=41;
 final store=w.TableRetain(4096,List.filled(64,0));
 final report2=w.TableWireReport();
 final loaded=w.variableWireLoadRetain(w.variableWireSave(value2),store,report2);
 check(loaded.value==41&&!report2.refused&&!report2.malformed,'scalar-only ordinary table remains variable');
 final saved=w.variableWireSaveRetain(loaded,store,w.TableWireReport());
 check(saved.length==w.variableWireMeasureRetain(loaded,store)&&w.variableWireLoad(saved,w.TableWireReport()).value==41,'variable retained save and measure');
`
			}
			source += "}\n"
			runFormsProbe(t, files, source)
			dir := t.TempDir()
			writeFiles(t, dir, files)
			clean := exec.Command(dartBinary(t), "analyze", ".")
			clean.Dir = dir
			if out, err := clean.CombinedOutput(); err != nil {
				t.Fatalf("generated fixed refusal libraries must analyze cleanly: %v\n%s", err, out)
			}
			// Every refusal exists but is non-callable: missing symbols or a callable
			// no-op would violate the API boundary just as accepting retention does.
			for _, verb := range []string{"w.fixedWireLoadRetain", "w.fixedWireMeasureRetain", "w.fixedWireSaveRetain", "f.fixedLoadRetainMessages", "f.fixedSaveRetainMessages"} {
				prefix, file := "w", "ProbeWire.dart"
				if strings.HasPrefix(verb, "f.") {
					prefix, file = "f", "ProbeFixed.dart"
				}
				probe := fmt.Sprintf("import %q as %s;\nvoid main(){%s();}\n", file, prefix, verb)
				if err := os.WriteFile(filepath.Join(dir, "refusal.dart"), []byte(probe), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(dartBinary(t), "analyze", "refusal.dart")
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "invocation_of_non_function") {
					t.Fatalf("%s must be a named non-callable refusal: %v\n%s", verb, err, out)
				}
			}
		})
	}
}
