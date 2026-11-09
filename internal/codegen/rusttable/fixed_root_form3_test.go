package rusttable

import (
	"strings"
	"testing"
)

// TestRustFixedRootSavesFormThree pins the STRICT-100 cell "Rust saving a fixed
// root" (docs/SPEC-TABLES.md §3.4): a fixed root's file save writes form byte 3,
// and the strict read — one read, the whole value or a refusal — reads the form
// byte FIRST and refuses anything a fixed reader does not carry. A form-1 input
// is previous_form, not a value; a form-2 batch is message_form_as_file; any
// other byte is newer_form.
func TestRustFixedRootSavesFormThree(t *testing.T) {
	t.Parallel()
	files := generate(t, valueOnly)
	fixed := string(files["probe_fixed.rs"])
	runtime := string(files[FixedRuntimeModule+".rs"])

	if !strings.Contains(runtime, "pub const TABLE_FIXED_FORM: u8 = 3;") {
		t.Errorf("the fixed runtime does not declare form byte 3 (TABLE_FIXED_FORM)")
	}

	// THE WRITER PUTS FORM 3 IN THE HEADER. A fixed root's save must be a
	// form-3 file, never a form-1 (variable) one.
	save := rustFn(fixed, "config_fixed_save")
	if !strings.Contains(save, "buffer[0] = TABLE_FIXED_FORM;") {
		t.Errorf("config_fixed_save does not write form byte 3; a fixed root must save as form 3")
	}

	// THE STRICT READ TAKES THE FORM BYTE FIRST (docs/SPEC-TABLES.md §3.4,
	// §5.3) and names the direction by where the byte sits relative to this
	// form: the whole load refuses, never a partial value.
	load := rustPrivateFn(fixed, "config_fixed_load_records")
	if !strings.Contains(load, "if data[0] != TABLE_FIXED_FORM {") {
		t.Errorf("config_fixed_load_records does not guard the form byte first")
	}
	for _, want := range []string{
		"1 => TableFixedReason::PreviousForm",
		"2 => TableFixedReason::MessageFormAsFile",
		"_ => TableFixedReason::NewerForm",
	} {
		if !strings.Contains(load, want) {
			t.Errorf("the strict read does not name %q for a non-3 form byte", want)
		}
	}
}

// TestRustFixedRootSavesFormThreeNegativeControl plants the fault this cell
// closes back into the real artifact — the save emitting form 1 instead of form
// 3 — and proves the form-3 check can go red. A check that cannot fail here
// would be vacuous and could never hold the rule.
func TestRustFixedRootSavesFormThreeNegativeControl(t *testing.T) {
	t.Parallel()
	fixed := string(generate(t, valueOnly)["probe_fixed.rs"])

	holds := func(src string) bool {
		save := rustFn(src, "config_fixed_save")
		return strings.Contains(save, "buffer[0] = TABLE_FIXED_FORM;")
	}
	if !holds(fixed) {
		t.Fatalf("the fixed save must write form byte 3 before the control can test it")
	}

	// THE PLANTED FAULT: the writer fills the header with the VARIABLE form
	// byte (1) instead of the fixed form's (3).
	save := rustFn(fixed, "config_fixed_save")
	planted := strings.Replace(save, "TABLE_FIXED_FORM", "TABLE_WIRE_FORM", 1)
	if holds(planted) {
		t.Errorf("the planted form-1 save still passes the check, so the check cannot go red")
	}
}
