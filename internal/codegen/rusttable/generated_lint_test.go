package rusttable

import (
	"slices"
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

fixed table Root
{
    value   Mixed
    payload bytes(1200)
}
`)
	message := string(files["probe_message.rs"])
	if !strings.Contains(message, "#[allow(clippy::large_const_arrays)]") {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		slices.Sort(names)
		t.Fatalf("message module lacks its narrow large_const_arrays allowance; files=%v", names)
	}
	if !strings.Contains(message, "flag: true,") {
		t.Fatal("a declared true bool default was not emitted as Rust bool true")
	}
	if strings.Contains(message, "value.flag = 1;") {
		t.Fatal("a declared true bool default was emitted as integer 1")
	}

	runtime := string(files[MessageRuntimeModule+".rs"])
	if !strings.Contains(runtime, "#[allow(clippy::large_const_arrays)]") {
		t.Fatal("message runtime lacks its narrow announcement-array allowance")
	}
}
