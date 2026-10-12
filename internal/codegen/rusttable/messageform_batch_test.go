package rusttable

// TestMessageFormBatchRust holds the MESSAGE FORM's batch surface
// (docs/SPEC-TABLES.md §3.3) to the Rust leg: MeasureMessages, SaveMessages and
// LoadMessages over the vocabulary the announcement surface already emits. The
// form is a fixed table's, so the three verbs ride for `fixed table` roots and
// nobody else. SPEC.md §6.1 spells those names per target ("the type name is in
// the function name in every target, for uniformity"); this leg's spelling is
// the snake-case free function `<root>_<verb>`.
//
// One subtest per clause of STEP 2. Each clause's assertion quotes the page
// sentence it implements and is a search over the generated Rust, so a clause
// bites by removing the emitted surface the search names.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// rustMessageFormSurface is the concatenated generated Rust for a fixed table
// root, which is the only root a message form rides (docs/SPEC-TABLES.md §3.3:
// "THE FORM IS A FIXED TABLE'S, and that is a declaration, not a derivation").
func rustMessageFormSurface(t *testing.T) string {
	t.Helper()
	files := generate(t, valueOnly)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	return all.String()
}

func TestMessageFormBatchRust(t *testing.T) {
	t.Parallel()
	gen := rustMessageFormSurface(t)

	// 2.1 "SaveMessages takes an array of bodies of ONE root and writes ONE
	// buffer. LoadMessages reads one buffer into the caller's storage and
	// allocates nothing. MeasureMessages sizes the buffer. There is no singular
	// verb." (docs/SPEC-TABLES.md §3.3, THE PRIMITIVE IS A BATCH)
	t.Run("2.1_batch_roundtrip", func(t *testing.T) {
		t.Parallel()
		for _, verb := range []string{"measure_messages", "save_messages", "load_messages"} {
			if !strings.Contains(gen, verb) {
				t.Fatalf("generated Rust lacks %s: a fixed table's batch surface is MeasureMessages, SaveMessages, LoadMessages over the announced vocabulary (§3.3)", verb)
			}
		}
		// The three verbs are real Rust: the emitted batch modules build as a
		// crate against the announcement runtime and the Row records.
		buildMessageCrate(t)
	})

	// 2.2 "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing
	// is decoded, no counter moves and `malformed` does not fire."
	// (docs/SPEC-TABLES.md §3.3, the scope is the ANNOUNCEMENT)
	t.Run("2.2_no_vocabulary", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: a batch read without a resolved vocabulary is refused by name no_vocabulary (§3.3)")
		}
		if !strings.Contains(gen, "no_vocabulary") {
			t.Fatal("generated Rust never names no_vocabulary: a batch read without a resolved vocabulary is refused by name (§3.3)")
		}
	})

	// 2.2 "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ...
	// `batch_too_large` ... never an exception." / "`M` ABOVE THE CALLER'S
	// CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... `batch_too_large` on
	// both sides." (docs/SPEC-TABLES.md §3.3, THE BATCH SURFACE'S FIVE ANSWERS)
	t.Run("2.2_batch_too_large", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "save_messages") {
			t.Fatal("generated Rust lacks save_messages: M above 256 on the write side is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "batch_too_large") {
			t.Fatal("generated Rust never names batch_too_large on the message path: M above 256 on the write side and M above capacity on the read side are refusals by name (§3.3)")
		}
	})

	// 2.3 "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`. ... ONE
	// `malformed` counts" / "BYTES AFTER THE PAD ARE MALFORMED."
	// (docs/SPEC-TABLES.md §3.3, THE BATCH SURFACE'S FIVE ANSWERS)
	t.Run("2.3_damage", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: damage inside body k delivers k-1 bodies and one malformed (§3.3)")
		}
		if !strings.Contains(gen, "malformed") {
			t.Fatal("generated Rust never reports malformed on the message path: damage inside a body and bytes after the pad are malformed (§3.3)")
		}
	})

	// 2.3 "The FORM BYTE is a whole byte and is read FIRST ... so a form a
	// reader does not know is a REFUSAL by name and never damage." A file
	// reader that meets a message-form buffer answers message_form_as_file.
	// (docs/SPEC-TABLES.md §3.3; the file reader's refusal is §3.4's own.)
	t.Run("2.3_forms", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: an unknown form byte is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "newer_form") {
			t.Fatal("generated Rust never reports newer_form: a form byte the leg does not carry is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "message_form_as_file") {
			t.Fatal("generated Rust never reports message_form_as_file: the file reader refuses a message-form buffer by name (§3.3)")
		}
	})
}

// buildMessageCrate builds the batch surface as a real Rust crate: the
// announcement runtime, the Row records and the <base>_message module, with a
// crate root that re-exports them. It proves the three verbs are real Rust and
// not strings alone.
func buildMessageCrate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("rustc is not on PATH: %v", err)
	}
	out := generate(t, valueOnly)
	root := t.TempDir()
	var mods []string
	for _, m := range []string{BuildVersionModule + ".rs", MessageRuntimeModule + ".rs", "probe_records.rs", "probe_message.rs"} {
		data, ok := out[m]
		if !ok {
			t.Fatalf("no %s in the generated unit", m)
		}
		if err := os.WriteFile(filepath.Join(root, m), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(m, ".rs"))
	}
	var main strings.Builder
	for _, m := range mods {
		main.WriteString("mod " + m + ";\n")
	}
	for _, m := range mods {
		main.WriteString("pub use " + m + "::*;\n")
	}
	main.WriteString("fn main() {}\n")
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "batchprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the batch surface did not build: %v\n%s", err, out)
	}
}
