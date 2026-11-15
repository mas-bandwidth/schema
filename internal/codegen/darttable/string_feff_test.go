package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const feffTableSchema = `package probe

union Pick { text string(16) }

table Root {
  name string(16)
  blob *string
  names [..4]string(16)
  pick Pick
}
`

func feffProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, feffTableSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, suffix := range []string{"A", ""} {
		input := fmt.Sprintf(`{"name":"\ufeff%s","blob":"\ufeff%s","names":["\ufeff%s"],"pick":{"text":"\ufeff%s"}}`, suffix, suffix, suffix, suffix)
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) || r.Refused {
			t.Fatalf("neutral FEFF input %q: %+v", input, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		expected := "\ufeff" + suffix
		fmt.Fprintf(&b, `{final data=bytes(%q);
final r=w.TableWireReport();final v=w.rootWireLoad(data,r);
check(!r.refused&&r.reason=='ok','FEFF load reason');
check(v.name==%q,'string field keeps leading U+FEFF');
check(v.blob==%q,'string blob keeps leading U+FEFF');
check(v.names.length==1&&v.names[0].value==%q,'text-list element keeps leading U+FEFF');
check(v.pick.armTag==1&&v.pick.text==%q,'union string arm keeps leading U+FEFF');
final saved=w.rootWireSave(v);
check(hex(saved)==%q,'FEFF load then save is byte-identical');
final authored=w.rootFromJson(%q);
check(hex(w.rootWireSave(authored))==%q,'FEFF authored wire matches the Go engine');
}
`, hex.EncodeToString(wire), expected, expected, expected, expected, hex.EncodeToString(wire), input, hex.EncodeToString(wire))
	}
	b.WriteString("}\n")
	return files, b.String()
}

func runFeffProbe(t *testing.T, files map[string][]byte, source string) error {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("feff probe %v: %w\n%s", args, err, out)
		}
	}
	return nil
}

func TestDartWireStringKeepsLeadingFeff(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	files, source := feffProbe(t)
	if err := runFeffProbe(t, files, source); err != nil {
		t.Fatal(err)
	}
}

func TestDartWireStringKeepsLeadingFeffNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	files, source := feffProbe(t)
	const fixed = "v.name = _decodeText(r.data, r.pos, r.pos + keep)"
	const reverted = "v.name = utf8.decode(r.data.sublist(r.pos, r.pos + keep))"
	wire := string(files["ProbeWire.dart"])
	if strings.Count(wire, fixed) != 1 {
		t.Fatalf("emitted string field decode site: %d occurrences of %q", strings.Count(wire, fixed), fixed)
	}
	files["ProbeWire.dart"] = []byte(strings.Replace(wire, fixed, reverted, 1))
	if err := runFeffProbe(t, files, source); err == nil {
		t.Fatal("a value site that drops a leading U+FEFF still passes the FEFF probe")
	}
}
