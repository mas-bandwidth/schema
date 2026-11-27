package rusttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// TestRustWriteRefusalEmitted pins the write contract's refusals in the
// variable-form save (docs/SPEC.md §5.1): a ranged integer off its min/max
// (row 1), a bits(N) value wider than its width (row 2), a counted array's
// count past Max (row 3, debug; release clamps to Max) and a bounded string(N)'s length past N
// or a zero byte (rows 4 and 8). A fixed field is stored raw, so its assert
// holds the whole-unit bounds shifted by F onto the raw scale. Each is named
// by the field it guards, so a regressed check fails by name.
func TestRustWriteRefusalEmitted(t *testing.T) {
	t.Parallel()
	out := generate(t, `package probe

table Config
{
    amount  int32 = 0 | min = 0, max = 10
    channel bits(6)
    items   [..8]int32
    label   string(16)
    scale   fixed(16, 16) = 0 | min = -8, max = 8
}
`)
	var src strings.Builder
	for _, data := range out {
		src.Write(data)
	}
	whole := src.String()
	for _, want := range []string{
		`debug_assert!(v.amount >= 0 && v.amount <= 10, "amount out of range [0, 10]");`,
		`debug_assert!(v.channel <= 63, "channel exceeds bits(6) width");`,
		`debug_assert!(v.items.len() <= 8, "items count past 8");`,
		`debug_assert!(v.label.len() <= 16, "label length past 16");`,
		`debug_assert!(!v.label.as_bytes().contains(&0), "label holds a zero byte");`,
		`debug_assert!(v.scale >= -524288 && v.scale <= 524288, "scale raw value out of range [-524288, 524288]");`,
	} {
		if !strings.Contains(whole, want) {
			t.Errorf("the variable-form save does not emit %q", want)
		}
	}
	// RUST WRITE CHECKS ARE DEBUG ASSERTS ONLY: the count past Max is clamped
	// for the storage the writer indexes, and no release build refuses it.
	if strings.Contains(whole, "count past max") {
		t.Error("the variable-form save still refuses a count past Max in a release build")
	}
}

// TestRustFixedAndMessageWriteRefusalEmitted pins the same contract on the
// fixed and message writers (docs/SPEC.md §5.1): FixedSave owes rows 1–6 and
// 8 in debug, SaveMessages rows 1–3 and 8 in debug, and a fixed/ufixed value
// is compared on its raw scale. Each check is named by the field it guards.
func TestRustFixedAndMessageWriteRefusalEmitted(t *testing.T) {
	t.Parallel()
	out := generate(t, `package probe

enum Grade { Bronze, Silver, Gold }

union Effect
{
    bonus  int32 | min = 0, max = 10
    malus  int32
}

fixed table Config
{
    amount  int32 = 0 | min = 0, max = 10
    channel bits(6)
    items   [..8]int32
    label   string(16)
    scale   fixed(16, 16) = 0 | min = -8, max = 8
    grade   Grade
    effect  Effect
}
`)
	fixed := string(out["probe_fixed.rs"])
	for _, want := range []string{
		`debug_assert!(value.tag <= 2, "Effect tag outside its arm set");`,
		`debug_assert!(value.amount >= 0 && value.amount <= 10, "amount out of range [0, 10]");`,
		`debug_assert!(value.channel <= 63, "channel exceeds bits(6) width");`,
		`value.items_count >= 0 && value.items_count <= 8,`,
		`debug_assert!(table_utf8_valid(&value.label[..n]), "label is ill-formed text");`,
		`debug_assert!(value.scale >= -524288 && value.scale <= 524288, "scale raw value out of range [-524288, 524288]");`,
		`debug_assert!(value.grade <= 3, "grade ordinal exceeds the enum's extent");`,
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("the fixed-form save does not emit %q", want)
		}
	}
	message := string(out["probe_message.rs"])
	for _, want := range []string{
		`debug_assert!(value.amount >= 0 && value.amount <= 10, "amount out of range [0, 10]");`,
		`debug_assert!(value.channel <= 63, "channel exceeds bits(6) width");`,
		`debug_assert!(table_message_utf8(&value.label[..value.label_length as usize]), "label is ill-formed text");`,
		`debug_assert!(value.scale >= -524288 && value.scale <= 524288, "scale raw value out of range [-524288, 524288]");`,
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the message-form save does not emit %q", want)
		}
	}
}

const fixedDeprecatedEnumSchema = `package probe

enum Phase
{
    Live
    Old | deprecated
    Next
}

fixed table Config
{
    few   [2..4]int32
    phase Phase
}
`

// TestRustFixedDeprecatedEnumWriteRefused pins the §2.15 debug assertion on
// a fixed Row's deprecated enum ordinal.
func TestRustFixedDeprecatedEnumWriteRefused(t *testing.T) {
	t.Parallel()
	fixed := string(generate(t, fixedDeprecatedEnumSchema)["probe_fixed.rs"])
	want := `debug_assert!(value.phase != 2, "invalid_value: phase holds a deprecated variant");`
	if !strings.Contains(fixed, want) {
		t.Errorf("the fixed-form save does not emit %q", want)
	}
}

func TestRustFixedDeprecatedEnumWriteRefusedNative(t *testing.T) {
	keyedRequireRustc(t)
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", generate(t, fixedDeprecatedEnumSchema))
	const main = `
#![allow(dead_code, unused_imports)]
mod probe;
use probe::*;

fn main() {
    let measured = config_fixed_measure(1);
    for phase in [1, 3] {
        let row = ConfigRow { phase, ..ConfigRow::default() };
        let mut buffer = vec![0; measured];
        assert_eq!(config_fixed_save(&[row], &mut buffer), Some(measured));
    }
    let row = ConfigRow { phase: 2, ..ConfigRow::default() };
    let mut buffer = vec![0; measured];
    let _ = config_fixed_save(&[row], &mut buffer);
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	compile := testproc.Command(t, "rustc", "--edition", "2024", "-A", "warnings", "-o", "run", "main.rs")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	run := testproc.Command(t, filepath.Join(dir, "run"))
	run.Dir = dir
	if out, err := run.CombinedOutput(); err == nil || !strings.Contains(string(out), "invalid_value: phase holds a deprecated variant") {
		t.Fatalf("native debug save did not assert on the deprecated variant: %v\n%s", err, out)
	}
}
