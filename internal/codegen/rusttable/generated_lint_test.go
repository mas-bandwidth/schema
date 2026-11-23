package rusttable

import (
	"strings"
	"testing"
)

// The normal generated-code lint gate treats warnings as errors. Keep the two
// message-form spellings that Wave 3 exposed pinned at the emitter boundary so
// the full corpus gate is evidence, not the first place either regression is
// diagnosed.
func TestRustMessageGeneratedLintShapes(t *testing.T) {
	t.Parallel()

	files := generate(t, `package lintshape

type Mixed
{
    flag bool = true
}

flags Marks { Red, Green, Blue }

fixed table Root
{
    value   Mixed
    payload bytes(1200)
    marks   Marks
    flux    int64 | min = -5000000000, max = 5000000000
}
`)
	message := string(files["probe_message.rs"])
	if !strings.Contains(message, "static ROOT_MESSAGE_DEFAULTS:") {
		t.Fatal("message defaults are not immutable static storage")
	}
	if !strings.Contains(message, "flag: true,") {
		t.Fatal("a declared true bool default was not emitted as Rust bool true")
	}
	if strings.Contains(message, "value.flag = 1;") {
		t.Fatal("a declared true bool default was emitted as integer 1")
	}
	if strings.Contains(message, "marks as u64") {
		t.Fatal("a u64 flags value was redundantly cast to u64")
	}
	block := string(files["probe_block.rs"])
	if strings.Contains(block, "flux as i64") {
		t.Fatal("an i64 ranged block value was redundantly cast to i64")
	}

	runtime := string(files[MessageRuntimeModule+".rs"])
	if !strings.Contains(runtime, "#[allow(clippy::large_const_arrays)]") {
		t.Fatal("message runtime lacks its narrow announcement-array allowance")
	}
}
