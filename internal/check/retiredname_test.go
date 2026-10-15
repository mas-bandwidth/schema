// Tests for refusing the reuse of a retired field, table, or enum-value name
// in the same scope with a new meaning across tables.baseline and schema.lock
// (docs/VERSIONING.md, docs/SPEC.md §4.1, §4.6, §4.7).
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func checkDiskUnit(t *testing.T, dir string, src string) (*ir.Unit, []error, []string) {
	t.Helper()
	path := filepath.Join(dir, "Unit.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(path, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, errs := Unit([]SourceFile{{
		Path:  path,
		Name:  "Unit.schema",
		Base:  "Unit",
		Bytes: []byte(src),
		AST:   f,
	}})
	return u, errs, []string{path}
}

func mustCheckDiskUnit(t *testing.T, dir string, src string) (*ir.Unit, []string) {
	t.Helper()
	u, errs, paths := checkDiskUnit(t, dir, src)
	if len(errs) > 0 {
		t.Fatalf("expected clean check, got: %v", errs)
	}
	return u, paths
}

func mustRefuseDiskUnit(t *testing.T, dir string, src string, wantSubstrings ...string) {
	t.Helper()
	_, errs, _ := checkDiskUnit(t, dir, src)
	if len(errs) == 0 {
		t.Fatalf("expected check.Unit to refuse (%v), but it compiled clean:\n%s", wantSubstrings, src)
	}
	joined := ""
	for i, e := range errs {
		if i > 0 {
			joined += "\n"
		}
		joined += e.Error()
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(joined, want) {
			t.Errorf("diagnostic must contain %q; got:\n%s", want, joined)
		}
	}
}

func TestRetiredNameReuseRefused(t *testing.T) {
	t.Run("baseline_retired_field_readded_with_new_type_or_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const v1 = `package p

table Config
{
    heading int16
    damage  float32 = 21.0
}
`
		const v2Retired = `package p

table Config
{
    heading int16
}
`
		u1, paths := mustCheckDiskUnit(t, dir, v1)
		if _, _, err := baseline.Update(u1, paths, "initial baseline"); err != nil {
			t.Fatal(err)
		}
		u2, paths := mustCheckDiskUnit(t, dir, v2Retired)
		if _, _, err := baseline.Update(u2, paths, "retire Config.damage"); err != nil {
			t.Fatal(err)
		}

		// Re-added with another type (float32 -> int32).
		mustRefuseDiskUnit(t, dir, `package p

table Config
{
    heading int16
    damage  int32 = 5
}
`, "Config.damage", "retired")

		// Re-added with another default / meaning (21.0 -> 99.0).
		mustRefuseDiskUnit(t, dir, `package p

table Config
{
    heading int16
    damage  float32 = 99.0
}
`, "Config.damage", "retired")

		// Re-added under a new field name riding the retired wire id via `was`.
		mustRefuseDiskUnit(t, dir, `package p

table Config
{
    heading int16
    hp      int32 | was = "damage"
}
`, "Config.damage", "retired")
	})

	t.Run("baseline_retired_table_readded_with_new_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const v1 = `package p

table Bonus
{
    score int32 = 10
}

table Config
{
    heading int16
    bonus   *Bonus
}
`
		const v2Retired = `package p

table Config
{
    heading int16
}
`
		u1, paths := mustCheckDiskUnit(t, dir, v1)
		if _, _, err := baseline.Update(u1, paths, "initial baseline"); err != nil {
			t.Fatal(err)
		}
		u2, paths := mustCheckDiskUnit(t, dir, v2Retired)
		if _, _, err := baseline.Update(u2, paths, "retire Bonus table"); err != nil {
			t.Fatal(err)
		}

		// Also record an explicit `table Bonus` row in `## retired` (which baseline.Parse supports).
		bpath := filepath.Join(dir, baseline.FileName)
		bdata, err := os.ReadFile(bpath)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := baseline.Parse(bpath, bdata)
		if err != nil {
			t.Fatal(err)
		}
		parsed.Retired = append(parsed.Retired, baseline.Retired{
			Vocab: "table",
			Name:  "Bonus",
			Id:    ir.TableWireId("Bonus"),
			Date:  "2026-10-15",
		})
		if err := os.WriteFile(bpath, []byte(parsed.Text()), 0o644); err != nil {
			t.Fatal(err)
		}

		// Re-adding retired table Bonus with a different field type.
		mustRefuseDiskUnit(t, dir, `package p

table Bonus
{
    score string(16)
}

table Config
{
    heading int16
    bonus   *Bonus
}
`, "Bonus", "retired")

		// Re-adding retired table Bonus with completely different fields.
		mustRefuseDiskUnit(t, dir, `package p

table Bonus
{
    label string(16)
}

table Config
{
    heading int16
    bonus   *Bonus
}
`, "Bonus", "retired")

		// Re-adding retired table Bonus as an enum in the same unit scope.
		mustRefuseDiskUnit(t, dir, `package p

enum Bonus { None, Extra }

table Config
{
    heading int16
    bonus   Bonus
}
`, "Bonus", "retired")

		// Re-adding retired table Bonus via `was = "Bonus"` with a new meaning.
		mustRefuseDiskUnit(t, dir, `package p

table Reward | was = "Bonus"
{
    label string(16)
}

table Config
{
    heading int16
    bonus   *Reward
}
`, "Bonus", "retired")
	})

	t.Run("baseline_retired_enum_value_readded_with_new_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const v1 = `package p

enum Grade { Bronze, Silver, Gold }

fixed table Badge
{
    grade Grade = Bronze
}

table Config
{
    grade Grade = Bronze
}
`
		const v2Retired = `package p

enum Grade { Bronze, Silver }

fixed table Badge
{
    grade Grade = Bronze
}

table Config
{
    grade Grade = Bronze
}
`
		u1, paths := mustCheckDiskUnit(t, dir, v1)
		if _, _, err := baseline.Update(u1, paths, "initial baseline"); err != nil {
			t.Fatal(err)
		}
		u2, paths := mustCheckDiskUnit(t, dir, v2Retired)
		if _, _, err := baseline.Update(u2, paths, "retire Grade.Gold"); err != nil {
			t.Fatal(err)
		}

		// Re-added with a new wire id via `was`.
		mustRefuseDiskUnit(t, dir, `package p

enum Grade
{
    Bronze
    Silver
    Gold | was = "LegacyGold"
}

fixed table Badge
{
    grade Grade = Bronze
}

table Config
{
    grade Grade = Bronze
}
`, "Grade.Gold", "retired")

		// Re-added under a new name riding the retired variant's wire id.
		mustRefuseDiskUnit(t, dir, `package p

enum Grade
{
    Bronze
    Silver
    Platinum | was = "Gold"
}

fixed table Badge
{
    grade Grade = Bronze
}

table Config
{
    grade Grade = Bronze
}
`, "Grade.Gold", "retired")

		// Re-added at a different ordinal on an enum reached by a fixed table / packet type.
		mustRefuseDiskUnit(t, dir, `package p

enum Grade { Bronze, Gold, Silver }

fixed table Badge
{
    grade Grade = Bronze
}

table Config
{
    grade Grade = Bronze
}
`, "Grade.Gold", "retired")
	})

	t.Run("lock_retired_field_readded_with_new_type_or_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const lockedSrc = `package p

fixed table ShipConfig
{
    name  string(32)
    speed float32
    armor uint8 | deprecated
}
`
		u, paths := mustCheckDiskUnit(t, dir, lockedSrc)
		if _, _, err := lockfile.Update(u, paths); err != nil {
			t.Fatal(err)
		}

		// Re-adding / repurposing the retired (`| deprecated`) field with a new type.
		mustRefuseDiskUnit(t, dir, `package p

fixed table ShipConfig
{
    name  string(32)
    speed float32
    armor uint32 | deprecated
}
`, "ShipConfig", "armor", "retired")

		// Re-adding / repurposing the retired field with a new default.
		mustRefuseDiskUnit(t, dir, `package p

fixed table ShipConfig
{
    name  string(32)
    speed float32
    armor uint8 = 7 | deprecated
}
`, "ShipConfig", "armor", "retired")

		// Re-adding the retired field name at a new slot with a new type.
		mustRefuseDiskUnit(t, dir, `package p

fixed table ShipConfig
{
    name  string(32)
    speed float32
    old   uint8 | deprecated
    armor float32
}
`, "ShipConfig", "armor", "retired")

		// Reusing the retired field's wire identity under a new field name via `was`.
		mustRefuseDiskUnit(t, dir, `package p

fixed table ShipConfig
{
    name   string(32)
    speed  float32
    shield uint16 | was = "armor"
}
`, "ShipConfig", "armor", "retired")
	})

	t.Run("lock_retired_table_readded_with_new_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const initial = `package p

fixed table Row
{
    a int32
}

fixed table Keep
{
    k int32
}
`
		u, paths := mustCheckDiskUnit(t, dir, initial)
		if _, _, err := lockfile.Update(u, paths); err != nil {
			t.Fatal(err)
		}
		if _, _, err := lockfile.Retire(u, paths, "Row", "Row is retired"); err != nil {
			t.Fatal(err)
		}

		// Dropping the retired table's declaration compiles cleanly.
		mustCheckDiskUnit(t, dir, `package p

fixed table Keep
{
    k int32
}
`)

		// Re-adding retired table Row with a different field type is refused by name.
		mustRefuseDiskUnit(t, dir, `package p

fixed table Row
{
    a string(16)
}

fixed table Keep
{
    k int32
}
`, "Row", "retired")

		// Re-adding retired table Row as a variable `table` is refused by name.
		mustRefuseDiskUnit(t, dir, `package p

table Row
{
    a int32
}

fixed table Keep
{
    k int32
}
`, "Row", "retired")

		// Re-adding retired table Row as a packet `type` is refused by name.
		mustRefuseDiskUnit(t, dir, `package p

type Row
{
    a int32
}

fixed table Keep
{
    k int32
}
`, "Row", "retired")

		// Re-adding retired table Row as an `enum` is refused by name.
		mustRefuseDiskUnit(t, dir, `package p

enum Row { First, Second }

fixed table Keep
{
    k int32
}
`, "Row", "retired")

		// Re-adding retired table Row via `was = "Row"` is refused by name.
		mustRefuseDiskUnit(t, dir, `package p

fixed table NewRow | was = "Row"
{
    a int64
}

fixed table Keep
{
    k int32
}
`, "Row", "retired")
	})

	t.Run("lock_retired_enum_value_readded_with_new_meaning", func(t *testing.T) {
		dir := t.TempDir()
		const initial = `package p

enum Grade { Bronze, Silver, Gold }

fixed table Row
{
    grade Grade
}

fixed table Keep
{
    k int32
}
`
		u, paths := mustCheckDiskUnit(t, dir, initial)
		if _, _, err := lockfile.Update(u, paths); err != nil {
			t.Fatal(err)
		}

		// Case 1: On a live locked enum, keeping the locked variant name Gold
		// while rebinding its wire identity via `was = "OldGold"` (a new meaning).
		mustRefuseDiskUnit(t, dir, `package p

enum Grade
{
    Bronze
    Silver
    Gold | was = "OldGold"
}

fixed table Row
{
    grade Grade
}

fixed table Keep
{
    k int32
}
`, "Grade", "Gold", "retired")

		// Case 2: Retire Row so Grade is a retired orphan in schema.lock, then re-use
		// Grade with Gold re-added at a different ordinal (ordinal 1 instead of 2).
		if _, _, err := lockfile.Retire(u, paths, "Row", "Row is retired"); err != nil {
			t.Fatal(err)
		}
		mustRefuseDiskUnit(t, dir, `package p

enum Grade { Bronze, Gold }

fixed table Keep
{
    k     int32
    grade Grade
}
`, "Grade", "Gold", "retired")
	})

	t.Run("same_meaning_allowed_only_where_versioning_md_says_so", func(t *testing.T) {
		dir := t.TempDir()
		const v1 = `package p

enum Grade { Bronze, Silver, Gold }

table Config
{
    heading int16
    damage  float32 = 21.0
    grade   Grade = Bronze
}
`
		const v2Retired = `package p

enum Grade { Bronze, Silver }

table Config
{
    heading int16
    grade   Grade = Bronze
}
`
		u1, paths := mustCheckDiskUnit(t, dir, v1)
		if _, _, err := baseline.Update(u1, paths, "initial baseline"); err != nil {
			t.Fatal(err)
		}
		u2, paths := mustCheckDiskUnit(t, dir, v2Retired)
		if _, _, err := baseline.Update(u2, paths, "retire damage and Gold"); err != nil {
			t.Fatal(err)
		}

		// Before `tables.baseline --update --reason` acknowledges the resurrection,
		// re-adding with the SAME meaning is still refused because `revived` is unset.
		mustRefuseDiskUnit(t, dir, v1, "Config.damage", "retired")

		// Acknowledge the same-meaning resurrection in tables.baseline (as
		// `schema tables-baseline --update --reason "..."` writes it).
		f, perrs := parser.Parse("Unit.schema", []byte(v1))
		if len(perrs) > 0 {
			t.Fatal(perrs[0])
		}
		inMemUnit, cerrs := Unit([]SourceFile{{
			Path:  "Unit.schema",
			Name:  "Unit.schema",
			Base:  "Unit",
			Bytes: []byte(v1),
			AST:   f,
		}})
		if len(cerrs) > 0 {
			t.Fatalf("in-memory unit without baseline: %v", cerrs)
		}
		if _, rewrote, err := baseline.Update(inMemUnit, paths, "acknowledge same-meaning resurrection"); err != nil || !rewrote {
			t.Fatalf("baseline.Update: rewrote=%v err=%v", rewrote, err)
		}

		// Now `tables.baseline` has `revived=...` and the same meaning in the baseline:
		// re-adding with the SAME meaning is allowed where docs/VERSIONING.md says so.
		mustCheckDiskUnit(t, dir, v1)

		// Even with `revived=...` in `tables.baseline`, changing the revived field or
		// enum variant to a NEW meaning is refused by name!
		mustRefuseDiskUnit(t, dir, `package p

enum Grade { Bronze, Silver, Gold }

table Config
{
    heading int16
    damage  int32 = 21
    grade   Grade = Bronze
}
`, "Config.damage", "retired")

		mustRefuseDiskUnit(t, dir, `package p

enum Grade
{
    Bronze
    Silver
    Gold | was = "OldGold"
}

table Config
{
    heading int16
    damage  float32 = 21.0
    grade   Grade = Bronze
}
`, "Grade.Gold", "retired")

		// In schema.lock, table retirement is one-way: re-adding a retired fixed table
		// even with the identical body is refused.
		lockDir := t.TempDir()
		const lockV1 = `package p

fixed table Row
{
    a int32
}

fixed table Keep
{
    k int32
}
`
		lu, lpaths := mustCheckDiskUnit(t, lockDir, lockV1)
		if _, _, err := lockfile.Update(lu, lpaths); err != nil {
			t.Fatal(err)
		}
		if _, _, err := lockfile.Retire(lu, lpaths, "Row", "Row is retired permanently"); err != nil {
			t.Fatal(err)
		}
		mustRefuseDiskUnit(t, lockDir, lockV1, "Row", "retired")
	})

	t.Run("versioning_md_evolution_table_row_updated", func(t *testing.T) {
		data, err := os.ReadFile("../../docs/VERSIONING.md")
		if err != nil {
			t.Fatal(err)
		}
		var matched string
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "|") &&
				strings.Contains(line, "a retired name re-added with a new meaning") {
				matched = line
				break
			}
		}
		if matched == "" {
			t.Fatal("docs/VERSIONING.md is missing the evolution-table row 'a retired name re-added with a new meaning'")
		}
		if strings.Contains(matched, "passes today") {
			t.Fatalf("docs/VERSIONING.md row 'a retired name re-added with a new meaning' still reads 'passes today':\n%s", matched)
		}
		cells := strings.Split(matched, "|")
		if len(cells) < 5 {
			t.Fatalf("unexpected table row format: %q", matched)
		}
		baselineCell := strings.TrimSpace(cells[3])
		for _, want := range []string{"refuses", "schema.lock", "same meaning"} {
			if !strings.Contains(baselineCell, want) {
				t.Fatalf("expected baseline cell to state %q, got %q", want, baselineCell)
			}
		}
		_ = fmt.Sprintf
	})
}
