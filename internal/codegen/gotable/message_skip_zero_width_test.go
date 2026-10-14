package gotable

import (
	"strings"
	"testing"
)

// TestGoMessageSkipOfZeroWidthElementsIsBounded pins the emitted Go message
// reader's element skip against a run of zero-width elements (SER-SCHEMA-04).
// A peer's announcement names an array of kind 14 with min = max = 0xFFFFFFFF
// whose element is kind 0: the skip must take the arithmetic branch at width
// 0 — never a per-element loop of up to 2^32-1 no-op iterations — and a
// nested element whose skip consumes zero bits without a fixed width is
// refused as malformed, as the shared engine's cstable reader does
// (cstable/messageread.go:51-52).
func TestGoMessageSkipOfZeroWidthElementsIsBounded(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	items [..3]int32
}
`
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		var text string
		for name, data := range generate(t, schema) {
			if strings.HasSuffix(name, "Table.go") {
				text = string(data)
			}
		}
		if text == "" {
			t.Fatal("no Table.go in the generated unit")
		}
		// Kind 0 and 32 elements skip arithmetically with width 0.
		if !strings.Contains(text, "if e.Element.Kind == 0 || e.Element.Kind == 32 {\n\t\t\twidth = 0\n\t\t}") {
			t.Fatalf("the emitted skip does not route element kinds 0 and 32 to the arithmetic branch")
		}
		// An element whose skip consumes zero bits without a fixed width is
		// refused as malformed, so no loop over it can run unbounded.
		if !strings.Contains(text, "at := r.Offset\n\t\t\tif !tableMessageSkip(r, v, indexBits, TableMessageEntry{Shape: e.Element}, depth+1) {\n\t\t\t\treturn false\n\t\t\t}\n\t\t\tif r.Offset == at {\n\t\t\t\treturn false\n\t\t\t}") {
			t.Fatalf("the emitted skip does not refuse a zero-bit-consuming element")
		}
	})
	t.Run("bounded", func(t *testing.T) {
		t.Parallel()
		// The hook counts the skip loop's element iterations; the bound is a
		// count, never a clock.
		const needle = "return r.Skip(int64(n) * width)\n\t\t}\n\t\tfor i := uint64(0); i < n; i++ {"
		edit := func(files map[string][]byte) {
			for name, data := range files {
				if !strings.HasSuffix(name, "Table.go") {
					continue
				}
				s := string(data)
				if !strings.Contains(s, needle) {
					t.Fatalf("the emitted skip lost its element loop in %s", name)
				}
				files[name] = []byte(strings.Replace(s, needle, needle+"\n\t\t\tmessageSkipHook()", 1))
			}
		}
		runGeneratedEdited(t, schema, `package probe

import "testing"

var messageSkipIterations uint64

func messageSkipHook() { messageSkipIterations++ }

func TestHostileAnnouncementIsBounded(t *testing.T) {
	// The hostile announcement: an array of kind 14 with min = max =
	// 0xFFFFFFFF whose element is kind 0, and a nested array whose inner
	// array is zero-width.
	hostile := TableMessageEntry{Id: 1, Shape: TableMessageShape{Kind: 14, Min: 0xFFFFFFFF, Max: 0xFFFFFFFF, Bits: -1}, Element: TableMessageShape{Kind: 0, Bits: -1}}
	nested := TableMessageEntry{Id: 2, Shape: TableMessageShape{Kind: 14, Min: 0, Max: 0xFFFFFFFF, Bits: -1}, Element: TableMessageShape{Kind: 14, Min: 0xFFFFFFFF, Max: 0xFFFFFFFF, Bits: -1}}
	var storage [2]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	vocab.Entries[0] = hostile
	vocab.Entries[1] = nested
	vocab.Count = 2
	vocab.RefBits = 1
	vocab.Announced = true
	r := TableBitReader{Buffer: make([]byte, 16)}
	if !tableMessageSkip(&r, &vocab, 0, hostile, 0) {
		t.Fatal("the zero-width element run was not skipped")
	}
	if messageSkipIterations > 1<<12 {
		t.Fatalf("the zero-width element run looped %d times", messageSkipIterations)
	}
	r = TableBitReader{Buffer: make([]byte, 16)}
	if tableMessageSkip(&r, &vocab, 0, nested, 0) {
		t.Fatal("the nested zero-width element was not refused")
	}
	if messageSkipIterations > 1<<12 {
		t.Fatalf("the nested zero-width element looped %d times", messageSkipIterations)
	}
}
`, edit)
	})
}
