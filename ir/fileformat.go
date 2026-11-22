// WHAT A FILE IS, BY ITS LEADING BYTES: the one classifier every tool path
// reads (docs/SPEC-TABLES.md §3, "THE FIRST BYTE"; §7.1; §19.1).
//
// A file schema writes begins one of two ways. The three live FORMS begin with
// their FORM BYTE (§3.4's registry, [WireForms]). The two ACCELERATORS, the
// cook and the block, begin with an eight-byte MAGIC of their own, stored in
// the producer's byte order, and carry no form byte today: bytes `4` and `5`
// are reserved for them and nothing writes either. So a tool that names a file
// reads the magic FIRST and the form byte second, and it is this table, not a
// switch in each command, that says which leading bytes mean which format.
//
// It lives beside the form-byte registry for the registry's own reason: which
// bytes mean which format is wire law, so the CLI, the cook's reader and every
// refusal that names a file read the one table.
package ir

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// CookMagic identifies a schema COOK, and it carries the byte-order check with
// it (docs/SPEC-TABLES.md §7.1). It is stored in the order the cook is produced
// in, so a consumer reads it BYTEWISE before anything else: it either is this
// constant, or is this constant byte-reversed, which identifies a cook of the
// other order, or is not a cook at all.
//
// The value is "SCHMCOOK" read as ASCII in the low-to-high byte order a
// little-endian store produces, exactly as [BlockMagic] is "SCHMABLK".
const CookMagic = uint64(0x4B4F4F434D484353)

// FileHeadBytes is how many leading bytes [ClassifyFile] reads: the width of a
// magic. A form byte is one of them.
const FileHeadBytes = 8

// FileKind is which of the formats schema writes a file is.
type FileKind uint8

// The kinds. FileUnknown is every file whose leading bytes belong to no
// format this build writes.
const (
	FileUnknown FileKind = iota
	FileVariable
	FileMessage
	FileFixed
	FileCook
	FileBlock
)

// FileFormat is one row of the classifier: a format schema writes, the leading
// bytes that identify it, and how a person validates a file of that format.
type FileFormat struct {
	Kind  FileKind
	Name  string // the name the spec gives it; a form's is its [WireForms] name
	Form  uint8  // the form byte, for a format identified by one; else 0
	Magic uint64 // the eight-byte magic, for a format identified by one; else 0
	Check string // the command that validates one, or why no command does
	Where string // the section of docs/SPEC-TABLES.md that defines the leading bytes
}

// FileFormats IS THE CLASSIFIER'S TABLE: every format a file schema writes can
// be. A writer whose output no row recognises is a writer whose files the
// tool's own `check` refuses, which is what cmd/schema's writer test holds.
var FileFormats = []FileFormat{
	{Kind: FileVariable, Name: "the variable form", Form: 1, Where: "§3",
		Check: "read it with `schema unpack --root <Table> --in <file> <tree-dir>`"},
	{Kind: FileMessage, Name: "the message form", Form: 2, Where: "§3.3",
		Check: "read it with `schema unpack --root <Table> --announce <announcement> --in <file> <tree-dir>`"},
	{Kind: FileFixed, Name: "the fixed form", Form: 3, Where: "§3.4",
		Check: "read it with `schema unpack --root <Table> --in <file> <tree-dir>`"},
	{Kind: FileCook, Name: "the cook", Magic: CookMagic, Where: "§7.1",
		Check: "validate it with `schema cook-check --root <Table> <file>`, adding `--attribution <file>` for a cook that carries data alone"},
	{Kind: FileBlock, Name: "the block form", Magic: BlockMagic, Where: "§19.1",
		Check: "no schema command reads a block: the generated BlockOpen validates it (§19.2)"},
}

// FileClass is the classifier's answer for one file.
type FileClass struct {
	Format *FileFormat // nil when the leading bytes belong to no format
	Big    bool        // a cook or a block written big-endian
}

// Kind is the format's kind, and FileUnknown for an unrecognised file.
func (c FileClass) Kind() FileKind {
	if c.Format == nil {
		return FileUnknown
	}
	return c.Format.Kind
}

// ClassifyFile names the format a file's leading bytes identify. It wants the
// first [FileHeadBytes] bytes and reads no more; a shorter head is classified
// by what it holds, so a one-byte head still names a form.
//
// THE MAGIC IS READ FIRST, bytewise in both orders, and the form byte second.
// The two cannot collide: no byte the registry assigns or reserves is the
// first byte of either magic in either order, which [FileFormats]'s own test
// holds.
func ClassifyFile(head []byte) FileClass {
	if len(head) >= FileHeadBytes {
		little := binary.LittleEndian.Uint64(head)
		big := binary.BigEndian.Uint64(head)
		for i := range FileFormats {
			f := &FileFormats[i]
			if f.Magic == 0 {
				continue
			}
			if little == f.Magic {
				return FileClass{Format: f}
			}
			if big == f.Magic {
				return FileClass{Format: f, Big: true}
			}
		}
	}
	if len(head) >= 1 {
		for i := range FileFormats {
			f := &FileFormats[i]
			if f.Magic == 0 && f.Form == head[0] {
				return FileClass{Format: f}
			}
		}
	}
	return FileClass{}
}

// magicText is a magic as the eight bytes a file of that order begins with,
// which is what a hex dump shows.
func magicText(magic uint64, big bool) string {
	var b [8]byte
	if big {
		binary.BigEndian.PutUint64(b[:], magic)
	} else {
		binary.LittleEndian.PutUint64(b[:], magic)
	}
	return string(b[:])
}

// Line is what a tool prints about a recognised file: the format by name, the
// leading bytes that say so, and the command that validates it.
func (c FileClass) Line() string {
	f := c.Format
	if f == nil {
		return ""
	}
	if f.Magic == 0 {
		return fmt.Sprintf("FORM %d %s (docs/SPEC-TABLES.md %s); %s", f.Form, f.Name, f.Where, f.Check)
	}
	order := "little"
	if c.Big {
		order = "big"
	}
	return fmt.Sprintf("%s, %s-endian: magic %s (docs/SPEC-TABLES.md %s); %s",
		strings.ToUpper(strings.TrimPrefix(f.Name, "the ")), order, magicText(f.Magic, c.Big), f.Where, f.Check)
}

// FileRecognised lists every leading byte sequence the classifier recognises,
// for the refusal of one it does not.
func FileRecognised() string {
	var rows []string
	for _, f := range FileFormats {
		if f.Magic == 0 {
			rows = append(rows, fmt.Sprintf("form byte %d (%s)", f.Form, f.Name))
		} else {
			rows = append(rows, fmt.Sprintf("the magic %s or %s (%s)", magicText(f.Magic, false), magicText(f.Magic, true), f.Name))
		}
	}
	return "recognised leading bytes: " + strings.Join(rows, ", ") +
		"; a cook's separated attribution part carries no header of its own and is read beside its cook by `schema cook-check --attribution`"
}

// FileLine is what a tool prints about a file's LEADING BYTES, before it says
// anything else about the file (docs/SPEC-TABLES.md §3). One line: the format
// and the command that validates it, or a refusal that names the first byte,
// says why it is refused and lists what is recognised.
//
// An empty file has no first byte, so it is refused for that and not for a
// byte it does not carry.
func FileLine(head []byte) string {
	if len(head) == 0 {
		return "REFUSED: an empty file carries no form byte (docs/SPEC-TABLES.md §3)"
	}
	if c := ClassifyFile(head); c.Format != nil {
		return c.Line()
	}
	b := head[0]
	f, assigned := WireFormOf(b)
	switch {
	case !assigned:
		return fmt.Sprintf("REFUSED: form %d is assigned by no form (docs/SPEC-TABLES.md §3 — the registry is the only place a value is assigned); %s", b, FileRecognised())
	case b == 0:
		return "REFUSED: form 0 is never assigned (docs/SPEC-TABLES.md §3); " + FileRecognised()
	default:
		// A RESERVED FORM BYTE. The format it is reserved for is implemented
		// and begins with its magic, so a file beginning with this byte is one
		// nothing writes, and the line says where the real one starts.
		for _, known := range FileFormats {
			if known.Name == f.Name {
				return fmt.Sprintf("FORM %d %s — PLANNED as a form byte, which no build writes or reads yet: %s today begins with the magic %s (docs/SPEC-TABLES.md %s)",
					b, f.Name, f.Name, magicText(known.Magic, false), known.Where)
			}
		}
		return fmt.Sprintf("FORM %d %s — PLANNED, no build writes or reads it yet (docs/SPEC-TABLES.md %s)", b, f.Name, f.Where)
	}
}
