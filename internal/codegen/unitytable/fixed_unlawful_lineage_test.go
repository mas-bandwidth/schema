package unitytable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityFixedUnlawfulLineage is the unity (C#) leg's answer to the
// UNLAWFUL lineage entry (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and EMIT,
// §5.4, §5.9 #30; docs/FIXED-FORM-VERSIONING-TESTS.md rows `unknown_census`
// and `kind_moved`). A handed-in older entry that carries a writer field this
// reader names nowhere — at the root, or inside every element of an array of
// tables — or a pair whose kind moved off every ladder lays no plan: a load of
// a file under its hash refuses `layout_malformed` (the writer's ?T into a bare
// T refuses `kind_mismatch`, ruling R2), moves no counter, and leaves every
// row handed in, poisoned first, at its declared defaults (SPEC-TABLES §6.5).
// A second load refuses the same way.
func TestUnityFixedUnlawfulLineage(t *testing.T) {
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect fixed unlawful lineage")
	t.Parallel()
	rows := []struct {
		name, older, newer, save, poison, check, reason string
	}{
		{
			name:   "unknown_census",
			older:  "package probe\ntype Item {\n    a int32\n    drop int32\n}\nfixed table C {\n    lead uint32 = 1\n    items [4]Item\n    trail uint32 = 2\n}\n",
			newer:  "package probe\ntype Item {\n    a int32\n}\nfixed table C {\n    lead uint32 = 1\n    items [4]Item\n    trail uint32 = 2\n}\n",
			save:   "        for (int i = 0; i < r.Length; i++) { r[i].Items.E0.A = 10; r[i].Items.E3.A = 13; r[i].Items.E3.Drop = 50; }",
			poison: "back[k].Lead = 99U; back[k].Trail = 88U; back[k].Items.E0.A = 5; back[k].Items.E3.A = 8;",
			check:  "back[k].Lead == 1U && back[k].Trail == 2U && back[k].Items.E0.A == 0 && back[k].Items.E3.A == 0",
			reason: "layout_malformed",
		},
		{
			name:   "unknown_root",
			older:  "package probe\nfixed table C {\n    lead uint32 = 1\n    extra uint16\n    trail uint32 = 2\n}\n",
			newer:  "package probe\nfixed table C {\n    lead uint32 = 1\n    trail uint32 = 2\n}\n",
			save:   "        for (int i = 0; i < r.Length; i++) { r[i].Lead = 7U; r[i].Extra = 9; }",
			poison: "back[k].Lead = 99U; back[k].Trail = 88U;",
			check:  "back[k].Lead == 1U && back[k].Trail == 2U",
			reason: "layout_malformed",
		},
		{
			name:   "kind_moved",
			older:  "package probe\nfixed table C {\n    lead uint32 = 1\n    v int32 = 0\n    trail uint32 = 2\n}\n",
			newer:  "package probe\nfixed table C {\n    lead uint32 = 1\n    v float32 = 0\n    trail uint32 = 2\n}\n",
			save:   "        for (int i = 0; i < r.Length; i++) { r[i].V = 7; }",
			poison: "back[k].Lead = 99U; back[k].V = 5.5f; back[k].Trail = 88U;",
			check:  "back[k].Lead == 1U && back[k].V == 0.0f && back[k].Trail == 2U",
			reason: "layout_malformed",
		},
		{
			name:   "optional_unwrapped",
			older:  "package probe\nfixed table C {\n    lead uint32 = 1\n    v ?int32\n    trail uint32 = 2\n}\n",
			newer:  "package probe\nfixed table C {\n    lead uint32 = 1\n    v int32 = 0\n    trail uint32 = 2\n}\n",
			save:   "",
			poison: "back[k].Lead = 99U; back[k].V = 5; back[k].Trail = 88U;",
			check:  "back[k].Lead == 1U && back[k].V == 0 && back[k].Trail == 2U",
			reason: "kind_mismatch",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			older := fixedproof.ParseCheckUnit(t, row.older)
			newer := fixedproof.ParseCheckUnit(t, row.newer)
			wire := unityRowSave(t, older, "C", row.save, 3)
			files, err := unityGenerateFiles(newer, unityRowLineage(t, older, "C"))
			if err != nil {
				t.Fatal(err)
			}
			probe := fmt.Sprintf(unityUnlawfulProbe, row.poison, row.check, row.reason)
			out, code, _, err := runUnityProbe(t, files, probe, map[string][]byte{"in.bin": wire})
			if err != nil {
				t.Fatalf("probe build/exec error: %v\n%s", err, out)
			}
			if code != 0 || !strings.Contains(out, "ROW unlawful: ok") {
				t.Fatalf("%s (exit %d):\n%s", row.name, code, out)
			}
		})
	}
}

const unityUnlawfulProbe = `using System;
using System.IO;
using Probe;
using static Probe.Schema;

static class Program
{
    static int Main()
    {
        byte[] data = File.ReadAllBytes("in.bin");
        C[] back = new C[8];
        for (int k = 0; k < back.Length; k++) { back[k] = Schema.NewC(); }
        for (int pass = 0; pass < 2; pass++)
        {
            for (int k = 0; k < back.Length; k++) { %[1]s }
            TableReport r = new TableReport { Unknown = 77, Widened = 88 };
            long n = Schema.CFixedLoad(back, data, ref r);
            if (n >= 0) { Console.WriteLine("ROW unlawful: FAILED pass " + pass + ": the unlawful entry LOADED " + n + " records"); return 1; }
            if (!r.Refused || TableReport.ReasonName(r.Reason) != "%[3]s")
            {
                Console.WriteLine("ROW unlawful: FAILED pass " + pass + ": not refused %[3]s: refused=" + r.Refused + " reason=" + TableReport.ReasonName(r.Reason));
                return 1;
            }
            if (r.Unknown != 0 || r.Widened != 0)
            {
                Console.WriteLine("ROW unlawful: FAILED pass " + pass + ": a refusal moved a counter: unknown=" + r.Unknown + " widened=" + r.Widened);
                return 1;
            }
            for (int k = 0; k < back.Length; k++)
            {
                if (!(%[2]s)) { Console.WriteLine("ROW unlawful: FAILED pass " + pass + ": row " + k + " is not at its declared defaults"); return 1; }
            }
        }
        Console.WriteLine("ROW unlawful: ok");
        return 0;
    }
}
`
