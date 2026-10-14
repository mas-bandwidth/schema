package darttable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Count parameters share begin's scope with its storage and refusal context;
// a legal schema name must not become a duplicate Dart local or parameter.
func TestBlockBuildDartCountParametersDoNotShadowRuntime(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
fixed table Row { value uint64 }
fixed table Root {
 refusal [..1]Row
 offset [..1]Row
 view [..1]Row
 used [..1]Row
 Base [..1]Row
 Bytes [..1]Row
}`)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	source := string(files["ProbeBlock.dart"])
	for _, name := range []string{"refusal", "offset", "view", "used", "base", "bytes"} {
		if !strings.Contains(source, "required int "+name+"Count") {
			t.Errorf("begin count for %s must use a distinct parameter name", name)
		}
	}
	if t.Failed() {
		return
	}
	t.Run("dart-compiles", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		// View/Bytes/Base also collide with pre-existing block class members.
		// Keep those in the parameter assertions above; compile the begin-only
		// collision cases here without broadening this fix into accessor naming.
		safeUnit := unitFrom(t, `package probe
fixed table Row { value uint64 }
fixed table Root {
 refusal [..1]Row
 offset [..1]Row
 used [..1]Row
}`)
		files, err := dart.Generate(safeUnit)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(safeUnit)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		const probe = `import 'dart:typed_data';
import 'ProbeBlock.dart' as b;
void main(){
 final bytes=Uint8List(b.RootBlock.blockMaxBytes);
 final refusal=b.TableBlockRefusal();
 final block=b.RootBlock.begin(bytes,0,refusalCount:1,offsetCount:1,usedCount:1,refusal:refusal);
 if(block==null||!refusal.empty)throw StateError('legal array names refused');
}
`
		if err := os.WriteFile(filepath.Join(dir, "block_names_probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "block_names_probe.dart"}, {"--enable-asserts", "block_names_probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("block count names %v: %v\n%s", args, err, out)
			}
		}
	})
}
