package luatable

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE RECORD-BODY BOUND (docs/FIXED-FORM-ALGORITHM.md §6, §1.1 rule 6;
// docs/SPEC-TABLES.md §3.4): a fixed record BODY of 65536 bytes is lawful and
// 65537 is not, measured on the body and never on the 8 + body a record
// carries with its layout hash. A table at the bound saves and loads through
// the generated Lua fixed form; a table one byte past it has no file form, so
// its save and its read refuse, and a file whose layout claims a 65537-byte
// body is refused by the reader. Only the verdict is compared
// (docs/rulings.json refusal-verdict-only).
//
// This is a functional test: it runs the pinned Lua interpreter, so it sits
// behind SCHEMA_SLOW=1. It takes no real time, opens no socket and uses no
// network.
const fixedBodyAtBoundSchema = `package probe

fixed table Edge
{
    b [65536]uint8
}
`

const fixedBodyPastBoundSchema = `package probe

fixed table Past
{
    b [65537]uint8
}
`

func TestLuaFixedRecordBodyBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")

	t.Run("a 65536-byte body saves and loads", func(t *testing.T) {
		t.Parallel()
		u, files := declaredFormsUnit(t, fixedBodyAtBoundSchema)
		probe := `local T = require("` + ModuleName(u) + `")
local v = T.NewEdge()
v.b[1] = 1
v.b[65536] = 171
local sok, file = pcall(T.FixedSave, "Edge", { v })
if not sok then
  print("save refused " .. tostring(file))
  return
end
local lok, rows = pcall(T.FixedLoad, "Edge", file)
if not lok then
  print("load refused " .. tostring(rows))
  return
end
print(string.format("rows %d first %d last %d count %d", #rows, rows[1].b[1], rows[1].b[65536], #rows[1].b))
local wok, one = pcall(T.WriteFixed, "Edge", v)
if not wok then
  print("write refused " .. tostring(one))
  return
end
local rok, back = pcall(T.ReadFixed, "Edge", one)
if not rok then
  print("read refused " .. tostring(back))
  return
end
print(string.format("read first %d last %d", back.b[1], back.b[65536]))
`
		got := strings.TrimSpace(string(declaredFormsRun(t, files, probe, nil)))
		want := "rows 1 first 1 last 171 count 65536\nread first 1 last 171"
		if got != want {
			t.Fatalf("a fixed record body of 65536 bytes is lawful (docs/FIXED-FORM-ALGORITHM.md §6)\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("a 65537-byte body has no file form", func(t *testing.T) {
		t.Parallel()
		u, files := declaredFormsUnit(t, fixedBodyPastBoundSchema)
		probe := `local T = require("` + ModuleName(u) + `")
local v = T.NewPast()
local sok = pcall(T.FixedSave, "Past", { v })
local wok = pcall(T.WriteFixed, "Past", v)
print(string.format("save %s write %s", sok and "read" or "refused", wok and "read" or "refused"))
`
		got := strings.TrimSpace(string(declaredFormsRun(t, files, probe, nil)))
		if got != "save refused write refused" {
			t.Fatalf("a fixed record body of 65537 bytes has no file form, so its save refuses (docs/SPEC-TABLES.md §3.4)\n--- got ---\n%s", got)
		}
	})

	t.Run("a file claiming a 65537-byte body is refused", func(t *testing.T) {
		t.Parallel()
		u, files := declaredFormsUnit(t, fixedBodyAtBoundSchema)
		save := `local T = require("` + ModuleName(u) + `")
local file = T.FixedSave("Edge", { T.NewEdge() })
io.write((file:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
`
		hexFile := strings.TrimSpace(string(declaredFormsRun(t, files, save, nil)))
		file := make([]byte, len(hexFile)/2)
		for i := range file {
			var b byte
			for _, c := range hexFile[2*i : 2*i+2] {
				b <<= 4
				switch {
				case c >= '0' && c <= '9':
					b |= byte(c - '0')
				default:
					b |= byte(c-'a') + 10
				}
			}
			file[i] = b
		}
		// the header is 20 bytes: form, seven reserved, the hash, the
		// layout length; the layout's entry count is four bytes, and its
		// root entry's size sits nine bytes into the entry
		layoutLen := int(binary.LittleEndian.Uint32(file[16:20]))
		if len(file) != 20+layoutLen+8+65536 {
			t.Fatalf("the saved file is %d bytes, want %d", len(file), 20+layoutLen+8+65536)
		}
		rootSize := 20 + 4 + 9
		if got := binary.LittleEndian.Uint32(file[rootSize:]); got != 65536 {
			t.Fatalf("the root entry's size is %d, want 65536", got)
		}
		forged := append([]byte(nil), file...)
		binary.LittleEndian.PutUint32(forged[rootSize:], 65537)
		forged = append(forged, 0)
		probe := `local T = require("` + ModuleName(u) + `")
local inputs = require("inputs")
local lok = pcall(T.FixedLoad, "Edge", inputs.file)
local rok = pcall(T.ReadFixed, "Edge", inputs.file)
print(string.format("load %s read %s", lok and "read" or "refused", rok and "read" or "refused"))
`
		got := strings.TrimSpace(string(declaredFormsRun(t, files, probe, map[string][]byte{"file": forged})))
		if got != "load refused read refused" {
			t.Fatalf("a file whose layout claims a 65537-byte body is refused (docs/FIXED-FORM-ALGORITHM.md §1.1 rule 6)\n--- got ---\n%s", got)
		}
	})
}
