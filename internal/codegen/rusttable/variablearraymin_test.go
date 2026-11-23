package rusttable

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE BIRTH VALUE OF A COUNTED ARRAY IN RUST (docs/SPEC-TABLES.md §22.2, the
// [M..N]T row; finding F-115; roadmap item
// sprint-out/rust-variable-class-carries-counted-array-minimums).
//
// The variable class carries a `[2..4]T`: it is born holding two default
// elements, and the variable and message writers elide exactly that value.
// The oracle is the Go engine (internal/tablewire encodeArray's allBirth and
// messageElides), whose bytes are pinned below: 020000 on the message form
// and a ten-byte variable file for the birth value; two elements with one
// live, and three default elements, ride. The pinned matrix carries the same
// rows for every element kind (test/verify-tablegen/pinned.go, pin:ci32:*).
const rustCountedBirthSchema = `package probe

fixed table FixRoot
{
    few [2..4]int32
}

table VarRoot
{
    few [2..4]int32
}
`

const rustCountedBirthMain = `
    fn hex(b: &[u8]) -> String { b.iter().map(|x| format!("{:02x}", x)).collect() }
    fn message(row: &FixRootRow) -> String {
        let one = std::slice::from_ref(row);
        let mut report = TableReport::default();
        let n = fix_root_measure_messages(one, &mut report);
        if n < 0 { return "refused".to_string(); }
        let mut out = vec![0u8; n as usize];
        if fix_root_save_messages(one, &mut out, &mut report) != n { return "refused".to_string(); }
        hex(&out)
    }
    let mut bad = 0;
    let mut row = |name: &str, got: String, want: &str| {
        if got == want { println!("ROW {}: ok", name); } else { println!("ROW {}: FAILED got {} want {}", name, got, want); bad += 1; }
    };
    let fixed = FixRootRow::default();
    let var = VarRoot::default();
    row("fresh", format!("{}{}{:?}{:?}", fixed.few_count, var.few.len(), &fixed.few[..2], var.few), "22[0, 0][0, 0]");
    row("birth_msg", message(&fixed), "020000");
    row("birth_var", hex(&var_root_save(&var)), "01000000000000000000");
    let mut one = FixRootRow::default();
    one.few[1] = 7;
    row("min_one_msg", message(&one), "0200010000003800000000");
    let mut vone = VarRoot::default();
    vone.few[1] = 7;
    row("min_one_var", hex(&var_root_save(&vone)), "01010e0a0402000000000700000000198ceafe187dc6dc0100000000000000");
    let mut three = FixRootRow::default();
    three.few_count = 3;
    row("min_plus1_msg", message(&three), "020003000000000000000000000000");
    if bad != 0 { std::process::exit(1); }
`

func TestRustVariableCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, rustCountedBirthSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the variable class refused a counted array with a minimum: %v", err)
	}
	var table string
	for name, data := range files {
		if strings.HasSuffix(name, "_table.rs") {
			table += string(data)
		}
	}
	// the class is emitted and born at Min (the pure-Go half: the fast lane)
	if !strings.Contains(table, "pub struct VarRoot") || !strings.Contains(table, "(0..2).map(|_| 0i32).collect::<Vec<_>>()") {
		t.Fatalf("VarRoot is not carried born at Min:\n%s", table)
	}
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("rustc not on PATH")
	}
	out, err := runRustGeneratedFilesProbe(t, files, rustCountedBirthMain)
	if err != nil || strings.Contains(out, "FAILED") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, name := range []string{"fresh", "birth_msg", "birth_var", "min_one_msg", "min_one_var", "min_plus1_msg"} {
		if !strings.Contains(out, "ROW "+name+": ok") {
			t.Errorf("row %s did not run:\n%s", name, out)
		}
	}
}
