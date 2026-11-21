package javatable

// A COUNTED ARRAY IS BORN AT ITS MINIMUM AND ITS BIRTH VALUE ELIDES (F-115,
// docs/SPEC-TABLES.md §22.2). A `[2..4]int32` field is born holding two
// default elements; the message and variable writers leave the field out at
// exactly that value, as Go's reference does (`020000` on the message form, a
// 10-byte variable file), and write it once one element is live. Every Java
// write path is checked: the message save, the plain variable save, the save
// of a value read from `{}`, and the retain save (on a variable root, the
// only kind retention takes).

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const countedBirthSchema = `package probe

fixed table Root {
    few [2..4]int32
}

table Var {
    few [2..4]int32
}
`

const countedBirthDriver = `import probe.*;

public final class Main {
    static String hex(byte[] b) {
        StringBuilder s = new StringBuilder();
        for (byte x : b) s.append(String.format("%02x", x & 0xff));
        return s.toString();
    }

    static String msg(RootFixed.Value v) {
        var vals = new RootFixed.Value[] { v };
        var rep = new TableVocabulary.Report();
        int n = RootMessage.measureMessages(vals, 1, rep);
        if (n < 1 || rep.refused) throw new AssertionError("measureMessages");
        byte[] out = new byte[n];
        if (RootMessage.saveMessages(vals, 1, out, rep) != n) throw new AssertionError("saveMessages");
        return hex(out);
    }

    static String retain(VarVariable.Value v) {
        var rep = new TableVariable.Report();
        byte[] out = VarRetain.saveRetain(v, new TableRetain(1 << 16, 1 << 10), rep);
        if (out == null || rep.refused) throw new AssertionError("saveRetain");
        return hex(out);
    }

    public static void main(String[] args) {
        var f = new RootFixed.Value();
        System.out.println("fresh_fixed " + f.fewCount + "," + f.few[0] + "," + f.few[1]);
        var r = new RootVariable.Value();
        System.out.println("fresh_root " + r.few.size() + "," + r.few.get(0) + "," + r.few.get(1));
        var v = new VarVariable.Value();
        System.out.println("fresh_var " + v.few.size() + "," + v.few.get(0) + "," + v.few.get(1));
        System.out.println("msg_birth " + msg(f));
        System.out.println("root_birth " + hex(RootVariable.save(r)));
        var jrep = new TableVariable.Report();
        var jr = RootVariable.fromJson("{}".getBytes(), jrep);
        if (jr == null || jrep.refused) throw new AssertionError("fromJson");
        System.out.println("json_birth " + hex(RootVariable.save(jr)));
        System.out.println("var_birth " + hex(VarVariable.save(v)));
        System.out.println("retain_birth " + retain(v));
        f.few[1] = 7;
        System.out.println("msg_live " + msg(f));
        r.few.set(1, 7);
        System.out.println("root_live " + hex(RootVariable.save(r)));
        v.few.set(1, 7);
        System.out.println("var_live " + hex(VarVariable.save(v)));
        System.out.println("retain_live " + retain(v));
    }
}
`

// countedBirthWant is Go's reference bytes for the two one-field roots at
// their birth value and with element 1 set to 7, on the message form (the
// fixed Root) and the variable form (both).
func countedBirthWant(t *testing.T) map[string]string {
	t.Helper()
	model := tabletext.NewModel(unitFrom(t, countedBirthSchema))
	want := map[string]string{"fresh_fixed": "2,0,0", "fresh_root": "2,0,0", "fresh_var": "2,0,0"}
	for _, root := range []string{"Root", "Var"} {
		st := model.Lookup(root)
		if st == nil {
			t.Fatalf("the reference unit has no %s", root)
		}
		inst := model.New(st)
		fv, ok := inst.FieldByKey("few")
		if !ok || fv.Count != 2 {
			t.Fatalf("the reference %s is not born at two elements", root)
		}
		for _, stage := range []string{"birth", "live"} {
			if stage == "live" {
				fv.Elems[1].I, fv.Elems[1].U = 7, 7
			}
			vr, err := tablewire.EncodeVariable(model, inst)
			if err != nil {
				t.Fatalf("EncodeVariable %s: %v", root, err)
			}
			if root == "Var" {
				want["var_"+stage] = hex.EncodeToString(vr)
				want["retain_"+stage] = hex.EncodeToString(vr)
				continue
			}
			want["root_"+stage] = hex.EncodeToString(vr)
			msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
			if err != nil {
				t.Fatalf("EncodeMessages: %v", err)
			}
			want["msg_"+stage] = hex.EncodeToString(msg)
		}
	}
	want["json_birth"] = want["root_birth"]
	if want["msg_birth"] != "020000" || len(want["root_birth"]) != 20 {
		t.Fatalf("the reference moved: message %s, variable %s (want 020000 and 10 bytes)", want["msg_birth"], want["root_birth"])
	}
	return want
}

func checkCountedBirth(t *testing.T, out string, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		got[k] = v
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q (Go's reference)", k, got[k], w)
		}
	}
}

// TestJavaCountedBirthElides holds the Java writers to Go's bytes for a
// counted array at its birth value and with one live element (F-115).
func TestJavaCountedBirthElides(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac")
	want := countedBirthWant(t)
	_, out := compileAndRunJavaProbe(t, countedBirthSchema, "probe", countedBirthDriver)
	checkCountedBirth(t, out, want)
}
