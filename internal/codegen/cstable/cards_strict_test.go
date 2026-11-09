package cstable_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCSharpCardsStrictReport pins the C# cards facade, driver, and tests
// (tables/cards/cs) to the strict read (docs/SPEC-TABLES.md §4, §6.5): one
// read returns the whole value or refuses the whole load. The hand-written
// C# sources must use the strict TableReport fields (Damaged, Refused,
// Unknown, Retained, RetainLost, Widened) and must not read any retired
// tolerant counter (KindMismatch, Clamped, Duplicate, or the Malformed
// verdict field). The conformance report spelling (KindMismatch=0,
// Clamped=0, Duplicate=0, Malformed=false) is an output format for
// accepted reads, not a report field, and is written as literals.
//
// TestCSharpCardsStrictReportNegativeControl plants each retired fault
// back into a copy of the real C# artifacts and requires the same check
// to go red, so the gate cannot pass vacuously.

var csCardsRetiredFields = []string{
	".KindMismatch",
	".Clamped",
	".Duplicate",
	".Malformed",
	"_tolerant",
	"Tolerant",
}

var csCardsRequiredCardsFragments = []string{
	"public static Record Load(byte[] wire, out TableReport report)",
	"public static Record ReadStrict(byte[] wire)",
	"public static Record FromJson(byte[] text)",
	"if (!Schema.CardLoad(card, wire, report) || report.Damaged || report.Refused)",
	"if (!Schema.CardFromJson(card, text, report) || report.Damaged || report.Refused)",
	"if (rep.Unknown > 0 || rep.RetainLost > 0 || rep.Damaged || rep.Refused)",
	"if (report.Damaged || report.Refused)",
}

const csCardsReportLiteral = `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`

func csCardsRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func csCardsReadSources(t *testing.T, root string) map[string]string {
	t.Helper()
	rels := []string{
		"Cards.cs",
		"Driver/Program.cs",
		"Tests/Program.cs",
	}
	out := make(map[string]string, len(rels))
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(root, "tables", "cards", "cs", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		out[rel] = string(b)
	}
	return out
}

func csCardsStrictCheck(sources map[string]string) []string {
	var bad []string
	for name, src := range sources {
		for _, retired := range csCardsRetiredFields {
			if strings.Contains(src, retired) {
				bad = append(bad, name+": uses retired report surface "+retired)
			}
		}
	}
	if src, ok := sources["Cards.cs"]; ok {
		for _, req := range csCardsRequiredCardsFragments {
			if !strings.Contains(src, req) {
				bad = append(bad, "Cards.cs: missing strict fragment "+req)
			}
		}
	} else {
		bad = append(bad, "Cards.cs: missing")
	}
	for _, name := range []string{"Driver/Program.cs", "Tests/Program.cs"} {
		if src, ok := sources[name]; ok {
			if !strings.Contains(src, csCardsReportLiteral) {
				bad = append(bad, name+": missing strict report literal "+csCardsReportLiteral)
			}
		} else {
			bad = append(bad, name+": missing")
		}
	}
	return bad
}

func TestCSharpCardsStrictReport(t *testing.T) {
	slowtest.Gate(t, "dotnet (builds and runs the C# cards facade tests and conformance driver)")
	root := csCardsRepoRoot(t)
	sources := csCardsReadSources(t, root)
	if bad := csCardsStrictCheck(sources); len(bad) != 0 {
		t.Fatalf("tables/cards/cs sources are not on the strict report:\n%s", strings.Join(bad, "\n"))
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "cards-cs-test", "cards-cs-conformance")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C# cards facade tests and conformance failed: %v\n%s", err, out)
	}
}

func TestCSharpCardsStrictReportNegativeControl(t *testing.T) {
	t.Parallel()
	root := csCardsRepoRoot(t)
	baseSources := csCardsReadSources(t, root)
	if bad := csCardsStrictCheck(baseSources); len(bad) != 0 {
		t.Fatalf("real tables/cards/cs sources are not on the strict report:\n%s", strings.Join(bad, "\n"))
	}

	sabotages := []struct {
		name string
		file string
		from string
		to   string
	}{
		{
			name: "retired-malformed-in-load",
			file: "Cards.cs",
			from: "if (!Schema.CardLoad(card, wire, report) || report.Damaged || report.Refused)",
			to:   "if (!Schema.CardLoad(card, wire, report) || report.Malformed || report.Refused)",
		},
		{
			name: "retired-kind-mismatch-in-driver-report",
			file: "Driver/Program.cs",
			from: csCardsReportLiteral,
			to:   `KindMismatch=" + r.KindMismatch + @"`,
		},
		{
			name: "retired-clamped-in-tests-report",
			file: "Tests/Program.cs",
			from: csCardsReportLiteral,
			to:   `Clamped=" + r.Clamped + @"`,
		},
		{
			name: "drop-damaged-from-refuse-lossy",
			file: "Cards.cs",
			from: "if (rep.Unknown > 0 || rep.RetainLost > 0 || rep.Damaged || rep.Refused)",
			to:   "if (rep.Unknown > 0 || rep.RetainLost > 0 || rep.Refused)",
		},
		{
			name: "drop-refused-from-json-read",
			file: "Cards.cs",
			from: "if (!Schema.CardFromJson(card, text, report) || report.Damaged || report.Refused)",
			to:   "if (!Schema.CardFromJson(card, text, report) || report.Damaged)",
		},
	}

	for _, tc := range sabotages {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutated := maps.Clone(baseSources)
			orig, ok := mutated[tc.file]
			if !ok {
				t.Fatalf("missing %s", tc.file)
			}
			if !strings.Contains(orig, tc.from) {
				t.Fatalf("%s: sabotage anchor %q not found in the real artifact", tc.file, tc.from)
			}
			mutated[tc.file] = strings.Replace(orig, tc.from, tc.to, 1)
			if mutated[tc.file] == orig {
				t.Fatalf("%s: sabotage %s planted nothing", tc.file, tc.name)
			}
			if bad := csCardsStrictCheck(mutated); len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): sabotaged %s stayed green", tc.name, tc.file)
			}
		})
	}
}
