package javatable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJavaJsonWideNumberPositions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK")
	const schema = `package probe
 enum Key { A, B }
 union Choice { arm int64 }
 table Root {
  plain int64
  values [..2]int64
  opt ?int64
  choice Choice
  lookup map[string]int64
  keyed [Key]int64
  narrow int32
  i128 int128 | min = -100, max = 100
  u128 uint128
  bits40 bits(40)
  fixed64 fixed(48,16) | min = -10, max = 10
  fixed128 fixed(96,32) | min = -10, max = 10
  fixed32 fixed(16,16) | min = -10, max = 10
 }
 `
	var src strings.Builder
	src.WriteString(`import java.nio.charset.StandardCharsets;
import probe.*;
public final class Main {
 static void test(String name, String text, boolean want) {
  TableVariable.Report r = new TableVariable.Report();
  RootVariable.Value v = RootVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
  boolean ok = v != null && !r.refused && !r.damaged;
  if (want ? !ok : (ok || !r.refused || r.reason == null || !r.reason.startsWith("kind_mismatch"))) {
   throw new AssertionError(name + " want=" + want + " refused=" + r.refused + " reason=" + r.reason);
  }
 }
 public static void main(String[] args) {
`)
	cases := []struct {
		name, text string
		want       bool
	}{
		{"plain-number", `{"plain":7}`, false}, {"plain-string", `{"plain":"7"}`, true},
		{"array-number", `{"values":[7]}`, false}, {"array-string", `{"values":["7"]}`, true},
		{"optional-number", `{"opt":7}`, false}, {"optional-string", `{"opt":"7"}`, true},
		{"union-number", `{"choice":{"arm":7}}`, false}, {"union-string", `{"choice":{"arm":"7"}}`, true},
		{"map-number", `{"lookup":{"x":7}}`, false}, {"map-string", `{"lookup":{"x":"7"}}`, true},
		{"keyed-number", `{"keyed":{"A":7}}`, false}, {"keyed-string", `{"keyed":{"A":"7"}}`, true},
		{"narrow-number", `{"narrow":7}`, true},
		{"i128-number", `{"i128":7}`, false}, {"i128-string", `{"i128":"7"}`, true},
		{"u128-number", `{"u128":7}`, false}, {"u128-string", `{"u128":"7"}`, true},
		{"bits40-number", `{"bits40":7}`, false}, {"bits40-string", `{"bits40":"7"}`, true},
		{"fixed64-number", `{"fixed64":1.5}`, false}, {"fixed64-string", `{"fixed64":"1.5"}`, true},
		{"fixed128-number", `{"fixed128":1.5}`, false}, {"fixed128-string", `{"fixed128":"1.5"}`, true},
		{"fixed32-number", `{"fixed32":1.5}`, true},
	}
	for _, c := range cases {
		fmt.Fprintf(&src, "test(%q,%q,%t);\n", c.name, c.text, c.want)
	}
	src.WriteString("} }\n")
	runJavaCarryProbe(t, schema, "", src.String())
}
