package luatable

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaAnnouncementLimitCheckedBeforeCopy pins the order of the
// announcement reader's vocabulary handling (docs/SPEC-TABLES.md §3.3): the
// vocabulary field's framing and its length are checked against the
// configured byte bound in place, and only a vocabulary inside the bound is
// copied out of the input. A string.sub counter in the Lua environment records
// the length of every substring taken; a refusal for size takes none of the
// vocabulary's length, a damaged frame still reports the framing refusal
// before the size refusal, and a vocabulary at exactly the bound is accepted.
func TestLuaAnnouncementLimitCheckedBeforeCopy(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	announce := readFile(t, filepath.Join(repo, "internal", "ci", "hostilecorpus", "corpus", "inputs", "announcement.bin"))
	vocabBytes := ir.TableVocabularyBytes(ir.TableVocabulary(unit))
	if len(vocabBytes) < 2 {
		t.Fatal("the examples unit announces no vocabulary")
	}
	at := bytes.Index(announce, vocabBytes)
	if at < 0 {
		t.Fatal("the vocabulary bytes are absent from the engine announcement")
	}
	lebLen := 1
	for n := len(vocabBytes); n >= 0x80; n >>= 7 {
		lebLen++
	}
	tag := at - lebLen - 1
	if tag < 0 || announce[tag] != 6 {
		t.Fatalf("the vocabulary field tag is not where its length puts it: %d", tag)
	}
	damaged := bytes.Clone(announce)
	damaged[tag] = 7

	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`
local lengths = {}
local realSub = string.sub
string.sub = function(s, i, j)
  local r = realSub(s, i, j)
  lengths[#lengths + 1] = #r
  return r
end
local T = require(%q)
local inputs = require("inputs")
local vocabLen = %d
local fieldLen = %d

local function copies(of)
  local n = 0
  for _, l in ipairs(lengths) do
    if l == of then n = n + 1 end
  end
  return n
end

-- over the bound: refused for size, nothing of the vocabulary copied
lengths = {}
local over = {}
local ro = { damaged = false, refused = 0 }
assert(not T.AnnounceRead(over, inputs.announce, ro, vocabLen - 1), "over the bound was accepted")
assert(ro.reason == "vocabulary_too_large", "reason: " .. tostring(ro.reason))
assert(not ro.damaged, "size refusal reported damage")
assert(copies(vocabLen) == 0, "the vocabulary was copied before the bound was checked")
assert(copies(fieldLen) == 0, "the vocabulary field was copied before the bound was checked")

-- a damaged frame over the bound still reports the framing refusal
lengths = {}
local bad = {}
local rd = { damaged = false, refused = 0 }
assert(not T.AnnounceRead(bad, inputs.damaged, rd, vocabLen - 1), "a damaged frame was accepted")
assert(rd.damaged == true, "a damaged frame did not report damage")
assert(rd.reason == nil, "a damaged frame reported: " .. tostring(rd.reason))

-- at exactly the bound: accepted, and the vocabulary is copied once
lengths = {}
local at = {}
local ra = { damaged = false, refused = 0 }
assert(T.AnnounceRead(at, inputs.announce, ra, vocabLen), "the vocabulary at the bound was refused")
assert(at.announced == true, "the vocabulary at the bound set no vocabulary")
assert(copies(vocabLen) == 1, "copies of the vocabulary at the bound: " .. copies(vocabLen))
print("announce-limit-ok")
`, ModuleName(unit), len(vocabBytes), len(vocabBytes)+lebLen+1))
	luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "damaged": damaged})
}
