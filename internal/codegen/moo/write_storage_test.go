package moo

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const writeStorageSchema = `package write_storage

type Cell
{
    value uint8
}

type Blob
{
    data bytes(4)
}

type Raw
{
    data [4]uint8
}

type Records
{
    cells [..2]Cell
}

type Values
{
    items [2]uint8
}
`

const writeStorageProbe = `--!strict
local serialize = require("./serialize")
local WriteStorage = require("./WriteStorage")

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local function attempt(name: string, write: (any) -> boolean)
	local stream = serialize.WriteStream.new(buffer.create(64))
	local ok, result = pcall(write, stream)
	stream:flush()
	local data = stream:data()
	if not ok then
		print(name .. " raised bytes=" .. buffer.len(data))
	elseif not result then
		print(name .. " refused bytes=" .. buffer.len(data))
	else
		print(name .. " written " .. hex(data))
	end
end

do
	local v = WriteStorage.NewBlob()
	v.Data = "not a buffer"
	v.DataLength = 1
	attempt("blob-string", function(s) return WriteStorage.WriteBlob(s, v) end)
	local n = WriteStorage.NewBlob()
	n.Data = nil
	attempt("blob-nil", function(s) return WriteStorage.WriteBlob(s, n) end)
	local good = WriteStorage.NewBlob()
	good.DataLength = 2
	buffer.writeu8(good.Data, 0, 0xAA)
	buffer.writeu8(good.Data, 1, 0xBB)
	attempt("blob-good", function(s) return WriteStorage.WriteBlob(s, good) end)
end

do
	local v = WriteStorage.NewRaw()
	v.Data = nil
	attempt("raw-nil", function(s) return WriteStorage.WriteRaw(s, v) end)
	local short = WriteStorage.NewRaw()
	short.Data = buffer.create(2)
	attempt("raw-short", function(s) return WriteStorage.WriteRaw(s, short) end)
	local good = WriteStorage.NewRaw()
	for i = 0, 3 do buffer.writeu8(good.Data, i, i + 1) end
	attempt("raw-good", function(s) return WriteStorage.WriteRaw(s, good) end)
end

do
	local v = WriteStorage.NewRecords()
	v.CellsCount = 1
	v.Cells[1] = nil
	attempt("records-nil-element", function(s) return WriteStorage.WriteRecords(s, v) end)
	local good = WriteStorage.NewRecords()
	good.CellsCount = 1
	good.Cells[1].Value = 5
	attempt("records-good", function(s) return WriteStorage.WriteRecords(s, good) end)
end

do
	local v = WriteStorage.NewValues()
	v.Items = 7
	attempt("values-number", function(s) return WriteStorage.WriteValues(s, v) end)
	local good = WriteStorage.NewValues()
	buffer.writeu8(good.Items, 0, 6)
	buffer.writeu8(good.Items, 1, 7)
	attempt("values-good", function(s) return WriteStorage.WriteValues(s, good) end)
end
`

// TestMooTypeWireWriteRefusesWrongStorage checks that the MOO packet writer
// refuses malformed storage before it writes any bytes (docs/PORT-MOO.md, THE
// WIRE; SPEC.md §5.1). Valid storage still emits the pinned byte sequences.
func TestMooTypeWireWriteRefusesWrongStorage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	dir, _ := generateInto(t, unitFromSource(t, "WriteStorage", writeStorageSchema))
	writeFile(t, dir, "main.moo", writeStorageProbe)
	out := strings.TrimSpace(string(moorun.Run(t, dir, "main.moo", nil)))
	want := strings.Join([]string{
		"blob-string refused bytes=0",
		"blob-nil refused bytes=0",
		"blob-good written 02aabb",
		"raw-nil refused bytes=0",
		"raw-short refused bytes=0",
		"raw-good written 01020304",
		"records-nil-element refused bytes=0",
		"records-good written 1500",
		"values-number refused bytes=0",
		"values-good written 0607",
	}, "\n")
	if out != want {
		t.Fatalf("storage write probe:\n%s\nwant:\n%s", out, want)
	}
}

// TestMooTypeWireWriteRefusesWrongStorageNegativeControl removes the emitted
// buffer type guard and requires the string case to raise, proving the main
// probe catches a missing storage check.
func TestMooTypeWireWriteRefusesWrongStorageNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	dir, files := generateInto(t, unitFromSource(t, "WriteStorage", writeStorageSchema))
	types := string(files["WriteStorage.moo"])
	anchor := "\tif type(value.Data) ~= \"buffer\" or "
	if strings.Count(types, anchor) != 1 {
		t.Fatalf("negative-control anchor matched %d times", strings.Count(types, anchor))
	}
	writeFile(t, dir, "WriteStorage.moo", strings.Replace(types, anchor, "\tif ", 1))
	writeFile(t, dir, "main.moo", writeStorageProbe)
	out := strings.TrimSpace(string(moorun.Run(t, dir, "main.moo", nil)))
	if !strings.Contains(out, "blob-string raised bytes=0") {
		t.Fatalf("negative control did not expose the missing storage guard:\n%s", out)
	}
}
