package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauUnlawfulAnswer is the Luau leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): Luau that poisons the row
// `r` of the NEW type with values that are not its declared defaults, and a
// Luau condition that is true when `r` is back at its declared defaults (read
// off the VNEW schema: lead = 1, trail = 2, every unlisted field its type's
// default).
type luauUnlawfulAnswer struct {
	poison string
	check  string
}

var luauUnlawfulAnswers = map[string]luauUnlawfulAnswer{
	"unknown_census": {
		poison: `r.Lead, r.Trail = 99, 88; for i = 1, 4 do r.Items[i].A = 5 + i end`,
		check:  `num(r.Lead) == 1 and num(r.Trail) == 2 and num(r.Items[1].A) == 0 and num(r.Items[2].A) == 0 and num(r.Items[3].A) == 0 and num(r.Items[4].A) == 0`,
	},
	"unknown_census_many": {
		poison: `r.Lead, r.Trail = 99, 88; for i = 1, 4 do r.Items[i].A = 5 + i end`,
		check:  `num(r.Lead) == 1 and num(r.Trail) == 2 and num(r.Items[1].A) == 0 and num(r.Items[2].A) == 0 and num(r.Items[3].A) == 0 and num(r.Items[4].A) == 0`,
	},
	"kind_moved": {
		poison: `r.Lead, r.V, r.Trail = 99, 5.5, 88`,
		check:  `num(r.Lead) == 1 and num(r.V) == 0 and num(r.Trail) == 2`,
	},
	"arm_removed": {
		poison: `r.Lead, r.Trail = 99, 88; r.Pick.Type = 2; r.Pick.Gamma.P = 77; r.Pick.Alpha.M = 66`,
		check:  `num(r.Lead) == 1 and num(r.Pick.Type) == 0 and num(r.Pick.Alpha.M) == 0 and num(r.Pick.Gamma.P) == 0 and num(r.Trail) == 2`,
	},
	"variant_removed": {
		poison: `r.Lead, r.Tier, r.Trail = 99, 1, 88`,
		check:  `num(r.Lead) == 1 and num(r.Tier) == 0 and num(r.Trail) == 2`,
	},
	"key_removed": {
		poison: `r.Lead, r.Trail = 99, 88; r.Slots[1], r.Slots[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.Slots[1]) == 0 and num(r.Slots[2]) == 0 and num(r.Trail) == 2`,
	},
	"array_shrunk": {
		poison: `r.Lead, r.Trail = 99, 88; r.Vals[1], r.Vals[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.Vals[1]) == 0 and num(r.Vals[2]) == 0 and num(r.Trail) == 2`,
	},
	"counted_shrunk": {
		poison: `r.Lead, r.Trail = 99, 88; r.ValsCount = 2; r.Vals[1], r.Vals[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.ValsCount) == 0 and num(r.Vals[1]) == 0 and num(r.Vals[2]) == 0 and num(r.Trail) == 2`,
	},
	"text_narrowed": {
		poison: `r.Lead, r.Trail = 99, 88; buffer.writestring(r.Text, 0, "zz"); r.TextLength = 2`,
		check:  `num(r.Lead) == 1 and num(r.TextLength) == 0 and buffer.len(r.Text) == 4 and buffer.readu32(r.Text, 0) == 0 and num(r.Trail) == 2`,
	},
	"bytes_narrowed": {
		poison: `r.Lead, r.Trail = 99, 88; buffer.writeu8(r.Blob, 0, 0x5A); r.BlobLength = 1`,
		check:  `num(r.Lead) == 1 and num(r.BlobLength) == 0 and buffer.len(r.Blob) == 4 and buffer.readu32(r.Blob, 0) == 0 and num(r.Trail) == 2`,
	},
	// the LAWFUL control: poison is the row's non-default values; check is
	// unused (the verdict is read off the landed values, luauLawfulBody).
	"lawful_control": {
		poison: `r.Lead, r.V, r.Trail = 99, 5, 88`,
	},
}

// TestLuauFixedUnlawfulCorpus is the Luau leg's driver for the shared
// unlawful-lineage corpus. Per case the NEW build, handed the OLD layout as a
// lineage entry (GenerateLineage), reads the OLD writer's file and refuses it
// before any record lands, every counter zero, every poisoned row handed in
// back at its declared defaults, on a first load and a second, through both the
// plain strict read and readInto (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and
// EMIT, §5.4, §5.9 #30; SPEC-TABLES §6.5). The refusal's NAME is this leg's
// own and is pinned here, in luauUnlawfulBody: layout_malformed
// (refusal-verdict-only: the shared harness compares verdicts, never names).
func TestLuauFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	fixedunlawful.Run(t, "luautable", luauUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a luauUnlawfulAnswer) string {
		t.Parallel()
		body := luauUnlawfulBody(c.Root, a.poison, a.check) + "if fails == 0 then print(\"VERDICT refused\") end\n"
		if c.Lawful {
			body = luauLawfulBody(c.Root, a.poison)
		}
		return luauVerdictProbe(t, c.New, []string{c.Old}, map[string][]byte{"file": luauCorpusFile(t, corpus, c.File)}, body)
	})
}

// luauLawfulBody is the LAWFUL control's probe body: the rows handed in are
// POISONED first, the file loads through readInto (and through the plain strict
// read, which must agree), and the probe prints the verdict, "loaded n: ...",
// built from the values the load landed in record 0.
func luauLawfulBody(root, poison string) string {
	return fmt.Sprintf(`local into = {}
for k = 1, 8 do
	into[k] = T.New%[1]s()
	local r = into[k]
	%[2]s
end
local function line(n: number, r: any): string
	return string.format("loaded %%d: lead=%%.0f v=%%.0f trail=%%.0f", n, num(r.Lead), num(r.V), num(r.Trail))
end
local rep: any = { unknown = 9, kindMismatch = 9, widened = 9 }
local got = F.readInto(T.%[1]sDescriptor, fixed, inputs.file, rep, into)
check(got ~= nil, "the LAWFUL control refused: " .. why(rep))
local strict: any = {}
local plain = LOAD("file", strict)
check(plain ~= nil, "the LAWFUL control refused the plain read: " .. why(strict))
if got ~= nil and plain ~= nil then
	-- readInto hands back the rows given, so the count is the plain read's;
	-- readInto must have landed record 0 only: row 2 stays poisoned
	local a, b = line(#plain, into[1]), line(#plain, plain[1])
	check(a == b, "readInto and the plain read disagree: " .. a .. " vs " .. b)
	check(num(into[2].Lead) == 99, "readInto landed more than the file's one record")
	if fails == 0 then print("VERDICT " .. a) end
end
`, root, poison)
}

// luauVerdictProbe is luauVersionProbe, answering the verdict the probe printed
// ("VERDICT <text>", printed only once every check in its body held).
func luauVerdictProbe(t *testing.T, reader string, older []string, inputs map[string][]byte, body string) string {
	t.Helper()
	u := luauVersionUnit(t, reader)
	lineage := map[string][]luautable.FixedLineageEntry{}
	for _, base := range older {
		o := luauVersionUnit(t, base)
		for _, st := range ir.TableFixedFormRoots(o) {
			e, ok := luautable.FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", reader, err)
	}
	tables, err := luautable.GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate %s: %v", reader, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := luauVersionRoot(t, u)
	var indented strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(body, "\n"), "\n") {
		if line != "" {
			indented.WriteString("\t" + line)
		}
		indented.WriteString("\n")
	}
	probe := fmt.Sprintf(luauVersionPrelude, reader+"Table", st.Name, strings.TrimRight(indented.String(), "\n"))
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", inputs)))
	if !strings.HasSuffix(out, "ok") || strings.Contains(out, "FAIL:") {
		t.Fatalf("%s probe:\n%s", reader, out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "VERDICT "); ok {
			return v
		}
	}
	t.Fatalf("%s probe printed no VERDICT:\n%s", reader, out)
	return ""
}
