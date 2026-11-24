package gotable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// goUnlawfulAnswer is the reference leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type goUnlawfulAnswer struct {
	// poison sets row back[i] of the NEW type to values that are not its
	// declared defaults, so a load that leaves a row alone is seen.
	poison string
	// write sets v[r] of the OLD type, for every record r, to the values the
	// reference dump wrote, so the Go writer's bytes are held to the corpus
	// file: the corpus is the Go writer's as well as the C++ dump's.
	write string
}

var goUnlawfulAnswers = map[string]goUnlawfulAnswer{
	"unknown_census": {
		poison: `back[i].Lead = 99; back[i].Trail = 88; for j := range back[i].Items { back[i].Items[j].A = int32(5 + j) }`,
		write:  `v[r].Lead = 1; v[r].Trail = 2; for j := range 4 { v[r].Items[j].A = int32(10 + j); v[r].Items[j].Drop = int32(900 + j) }`,
	},
	"unknown_census_many": {
		poison: `back[i].Lead = 99; back[i].Trail = 88; for j := range back[i].Items { back[i].Items[j].A = int32(5 + j) }`,
		write:  `v[r].Lead = 1; v[r].Trail = 2; for j := range 4 { v[r].Items[j].A = int32(10*(r+1) + j); v[r].Items[j].Drop = int32(900 + 10*r + j) }`,
	},
	"kind_moved": {
		poison: `back[i].Lead = 99; back[i].V = 5.5; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].V = 7; v[r].Trail = 0xBBBBBBBB`,
	},
	"arm_removed": {
		poison: `back[i].Lead = 99; back[i].Pick.Type = PickTypeGamma; back[i].Pick.Gamma.P = 77; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].Pick.Type = PickTypeGamma; v[r].Pick.Gamma.P = 9; v[r].Trail = 0xBBBBBBBB`,
	},
	"variant_removed": {
		poison: `back[i].Lead = 99; back[i].Tier = TierGold; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].Tier = TierGold; v[r].Trail = 0xBBBBBBBB`,
	},
	"key_removed": {
		poison: `back[i].Lead = 99; for j := range back[i].Slots { back[i].Slots[j] = int32(70 + j) }; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].Slots[0] = 11; v[r].Slots[1] = 22; v[r].Slots[2] = 33; v[r].Trail = 0xBBBBBBBB`,
	},
	"array_shrunk": {
		poison: `back[i].Lead = 99; for j := range back[i].Vals { back[i].Vals[j] = int32(70 + j) }; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; for j := range 4 { v[r].Vals[j] = int32(41 + j) }; v[r].Trail = 0xBBBBBBBB`,
	},
	"counted_shrunk": {
		poison: `back[i].Lead = 99; back[i].ValsCount = 2; back[i].Vals[0] = 70; back[i].Vals[1] = 71; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].ValsCount = 2; v[r].Vals[0] = 51; v[r].Vals[1] = 52; v[r].Trail = 0xBBBBBBBB`,
	},
	"text_narrowed": {
		poison: `back[i].Lead = 99; copy(back[i].Text[:], "zz"); back[i].TextLength = 2; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; copy(v[r].Text[:], "ab"); v[r].TextLength = 2; v[r].Trail = 0xBBBBBBBB`,
	},
	"bytes_narrowed": {
		poison: `back[i].Lead = 99; back[i].Blob[0] = 0x5A; back[i].BlobLength = 1; back[i].Trail = 88`,
		write:  `v[r].Lead = 0xAAAAAAAA; v[r].Blob[0] = 0xF0; v[r].Blob[1] = 0xF1; v[r].BlobLength = 2; v[r].Trail = 0xBBBBBBBB`,
	},
}

// TestFixedUnlawfulCorpus is the reference leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record, every counter zero,
// every poisoned row back at its declared defaults, on a first load and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4; SPEC-TABLES
// §6.5). The refusal's NAME is pinned here, layout_malformed, and is never
// compared across legs (refusal-verdict-only). And the Go writer, given the
// values the dump wrote, writes the corpus file byte for byte.
func TestFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	fixedunlawful.Run(t, "gotable", goUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a goUnlawfulAnswer) {
		t.Parallel()
		older := readSchema(t, c.Old)
		newer := readSchema(t, c.New)
		file := filepath.Join(corpus, c.File)
		load := fmt.Sprintf(`package probe

import ("errors"; "os"; "reflect"; "testing")

func TestUnlawful(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	want := make([]%[2]s, len(back))
	for i := range want { %[2]sReset(&want[i]) }
	plan := make([]TableFixedEntry, 4096)
	for attempt := range 2 {
		for i := range back { %[2]sReset(&back[i]); %[3]s }
		var r TableReport
		n := %[2]sFixedLoad(back, data, plan, &r)
		if n >= 0 {
			t.Fatalf("%[4]s attempt %%d: the UNLAWFUL entry LOADED %%d records: %%+v", attempt, n, back[0])
		}
		if !errors.Is(r.Err(), TableRefuseLayoutMalformed) {
			t.Fatalf("%[4]s attempt %%d: refused, but not layout_malformed: %%v", attempt, r.Err())
		}
		if r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0 {
			t.Fatalf("%[4]s attempt %%d: a refusal moves no counter: %%+v", attempt, r)
		}
		if !reflect.DeepEqual(back, want) {
			t.Fatalf("%[4]s attempt %%d: rows not at their declared defaults: %%+v", attempt, back[0])
		}
	}
}
`, file, c.Root, a.poison, c.Name)
		if out, err := runVersionProbe(t, newer, []string{older}, load); err != nil {
			t.Fatalf("%s: %v\n%s", c.Name, err, out)
		}
		write := fmt.Sprintf(`package probe

import ("bytes"; "os"; "testing")

func TestWriterIsTheCorpus(t *testing.T) {
	want, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	v := make([]%[2]s, %[3]d)
	for r := range v { %[2]sReset(&v[r]); %[4]s }
	got := make([]byte, %[2]sFixedMeasure(int64(len(v))))
	if n := %[2]sFixedSave(v, got); n != int64(len(got)) {
		t.Fatalf("%[5]s: the Go writer saved %%d of %%d bytes", n, len(got))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%[5]s: the Go writer's bytes are not the corpus file\n got %%x\nwant %%x", got, want)
	}
}
`, file, c.Root, c.Records, a.write, c.Name)
		if out, err := runVersionProbe(t, older, nil, write); err != nil {
			t.Fatalf("%s writer: %v\n%s", c.Name, err, out)
		}
	})
}
