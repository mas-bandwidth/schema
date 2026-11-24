package unitytable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// unityUnlawfulAnswer is the unity leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): a poison that sets row
// back[k] of the NEW struct to values that are not its declared defaults, and
// a check that is true when back[k] is at its declared defaults (lead = 1,
// trail = 2, every unlisted field its type's default).
type unityUnlawfulAnswer struct {
	poison string
	check  string
}

const unityCensusCheck = "back[k].Lead == 1U && back[k].Trail == 2U && back[k].Items.E0.A == 0 && back[k].Items.E1.A == 0 && back[k].Items.E2.A == 0 && back[k].Items.E3.A == 0"

var unityUnlawfulAnswers = map[string]unityUnlawfulAnswer{
	"unknown_census": {
		poison: "back[k].Lead = 99U; back[k].Trail = 88U; back[k].Items.E0.A = 5; back[k].Items.E1.A = 6; back[k].Items.E2.A = 7; back[k].Items.E3.A = 8;",
		check:  unityCensusCheck,
	},
	"unknown_census_many": {
		poison: "back[k].Lead = 99U; back[k].Trail = 88U; back[k].Items.E0.A = 5; back[k].Items.E1.A = 6; back[k].Items.E2.A = 7; back[k].Items.E3.A = 8;",
		check:  unityCensusCheck,
	},
	"kind_moved": {
		poison: "back[k].Lead = 99U; back[k].V = 5.5f; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].V == 0.0f && back[k].Trail == 2U",
	},
	"arm_removed": {
		poison: "back[k].Lead = 99U; back[k].Pick.Type = PickType.Gamma; back[k].Pick.Gamma.P = 77; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].Pick.Type == PickType.None && back[k].Pick.Alpha.M == 0 && back[k].Pick.Gamma.P == 0 && back[k].Trail == 2U",
	},
	"variant_removed": {
		poison: "back[k].Lead = 99U; back[k].Tier = Tier.Gold; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].Tier == Tier.None && back[k].Trail == 2U",
	},
	"key_removed": {
		poison: "back[k].Lead = 99U; back[k].Slots.E0 = 70; back[k].Slots.E1 = 71; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].Slots.E0 == 0 && back[k].Slots.E1 == 0 && back[k].Trail == 2U",
	},
	"array_shrunk": {
		poison: "back[k].Lead = 99U; back[k].Vals.E0 = 70; back[k].Vals.E1 = 71; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].Vals.E0 == 0 && back[k].Vals.E1 == 0 && back[k].Trail == 2U",
	},
	"counted_shrunk": {
		poison: "back[k].Lead = 99U; back[k].ValsCount = 2; back[k].Vals.E0 = 70; back[k].Vals.E1 = 71; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].ValsCount == 0 && back[k].Vals.E0 == 0 && back[k].Vals.E1 == 0 && back[k].Trail == 2U",
	},
	"text_narrowed": {
		poison: "back[k].Lead = 99U; back[k].Text.E0 = (byte)'z'; back[k].Text.E1 = (byte)'z'; back[k].TextLength = 2; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].TextLength == 0 && back[k].Text.E0 == 0 && back[k].Text.E1 == 0 && back[k].Text.E2 == 0 && back[k].Text.E3 == 0 && back[k].Trail == 2U",
	},
	"bytes_narrowed": {
		poison: "back[k].Lead = 99U; back[k].Blob.E0 = 0x5A; back[k].BlobLength = 1; back[k].Trail = 88U;",
		check:  "back[k].Lead == 1U && back[k].BlobLength == 0 && back[k].Blob.E0 == 0 && back[k].Blob.E1 == 0 && back[k].Blob.E2 == 0 && back[k].Blob.E3 == 0 && back[k].Trail == 2U",
	},
}

// TestUnityFixedUnlawfulCorpus is the unity leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file (the C++ reference's bytes) before any
// record, every counter zero, every poisoned row back at its declared
// defaults, on a first load and a second (docs/FIXED-FORM-ALGORITHM.md §5.2
// MATCH/EMIT, §5.4, §5.9 #30; SPEC-TABLES §6.5). The refusal's NAME,
// layout_malformed, is this leg's own and is pinned here; it is never compared
// across legs (ruling refusal-verdict-only).
func TestUnityFixedUnlawfulCorpus(t *testing.T) {
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect fixed unlawful corpus")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := filepath.Join(repo, "build", "fixedform-corpus")
	fixedunlawful.Run(t, "unitytable", unityUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a unityUnlawfulAnswer) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join(corpus, c.File))
		if err != nil {
			if slowtest.CorpusRequired() {
				t.Fatalf("the fixed-form corpus has no %s: %v (run make tables-fixedform-corpus)", c.File, err)
			}
			t.Skipf("the fixed-form corpus has no %s (make tables-fixedform-corpus): %v", c.File, err)
		}
		older := fixedproof.LoadRepoUnit(t, repo, "test/tables/"+c.Old+".schema")
		newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/"+c.New+".schema")
		files, err := unityGenerateFiles(newer, unityRowLineage(t, older, c.Root))
		if err != nil {
			t.Fatal(err)
		}
		probe := fmt.Sprintf(unityUnlawfulCorpusProbe, unityNamespace(newer.Package), c.Root, a.poison, a.check, c.Name)
		out, code, _, err := runUnityProbe(t, files, probe, map[string][]byte{"in.bin": data})
		if err != nil {
			t.Fatalf("%s: probe build/exec error: %v\n%s", c.Name, err, out)
		}
		if code != 0 || !strings.Contains(out, "ROW "+c.Name+": ok") {
			t.Fatalf("%s (exit %d):\n%s", c.Name, code, out)
		}
	})
}

// unityUnlawfulCorpusProbe: %[1]s the namespace, %[2]s the root, %[3]s the
// poison, %[4]s the defaults check, %[5]s the case. Two loads, each over rows
// poisoned first; each refuses layout_malformed by name, lands nothing, moves
// no counter and leaves every row at its declared defaults.
const unityUnlawfulCorpusProbe = `using System;
using System.IO;
using %[1]s;
using static %[1]s.Schema;

static class Program
{
    static int Main()
    {
        byte[] data = File.ReadAllBytes("in.bin");
        %[2]s[] back = new %[2]s[8];
        for (int k = 0; k < back.Length; k++) { back[k] = Schema.New%[2]s(); }
        for (int pass = 0; pass < 2; pass++)
        {
            for (int k = 0; k < back.Length; k++) { %[3]s }
            TableReport r = new TableReport { Unknown = 77, Widened = 88, Retained = 66, RetainLost = 55 };
            long n = Schema.%[2]sFixedLoad(back, data, ref r);
            if (n >= 0) { Console.WriteLine("ROW %[5]s: FAILED pass " + pass + ": the unlawful entry LOADED " + n + " records"); return 1; }
            if (!r.Refused || TableReport.ReasonName(r.Reason) != "layout_malformed")
            {
                Console.WriteLine("ROW %[5]s: FAILED pass " + pass + ": not refused layout_malformed: refused=" + r.Refused + " reason=" + TableReport.ReasonName(r.Reason));
                return 1;
            }
            if (r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                Console.WriteLine("ROW %[5]s: FAILED pass " + pass + ": a refusal moved a counter: unknown=" + r.Unknown + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
                return 1;
            }
            for (int k = 0; k < back.Length; k++)
            {
                if (!(%[4]s)) { Console.WriteLine("ROW %[5]s: FAILED pass " + pass + ": row " + k + " is not at its declared defaults"); return 1; }
            }
        }
        Console.WriteLine("ROW %[5]s: ok");
        return 0;
    }
}
`
