package tablewire

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ID TABLE'S MAXIMUM, ON THE WRITE SIDE (docs/SPEC-TABLES.md §3, ruling
// R8). Every reader refuses an id table of more than ir.TableWireIdTableBound
// entries as damage, so every writer must refuse (or, on the assert legs,
// debug-assert) a value that would need more, and no writer produces a file
// its readers refuse. A unit's id capacity is a compile-time fact the schema
// sets, and nothing caps it: a table of 4200 fields, all set, needs 4200 ids.
//
// IdCapSchema is the ONE schema every port's writer test generates, so each
// port is held to the same arithmetic:
//
//	an enum K of IdCapVariants variants, and
//	table Root { keys [K]uint8; x uint8; y uint8 }
//
// With every slot of `keys` set to 1 the value needs IdCapVariants + 1 ids
// (the variants and the field `keys`); `x` adds one and `y` one more. So:
//
//	keys all 1, x = 0, y = 0   needs IdCapKeysIds       (bound - 1)  writes
//	keys all 1, x = 1, y = 0   needs bound entries                   writes
//	keys all 1, x = 1, y = 1   needs bound + 1 entries               REFUSED
//
// The package is the schema's package name, `idcap`. A zero element of
// `keys` elides, so a test sets each slot to 1.
const (
	IdCapVariants = ir.TableWireIdTableBound - 2
	IdCapKeysIds  = IdCapVariants + 1
)

// IdCapSchema is the schema source for that unit.
func IdCapSchema() string {
	var b strings.Builder
	b.WriteString("package idcap\n\nenum K {\n")
	for i := 0; i < IdCapVariants; i++ {
		fmt.Fprintf(&b, "    V%d\n", i)
	}
	b.WriteString("}\n\ntable Root {\n    keys [K]uint8\n    x uint8\n    y uint8\n}\n")
	return b.String()
}

// THE ANNOUNCEMENT'S VOCABULARY, ON THE WRITE SIDE (docs/SPEC-TABLES.md §3.3,
// ruling R8 follow-up). A receiver refuses an announcement of more than its
// declared entry maximum, 4096 by default (ir.TableVocabularyMaxEntries), as
// vocabulary_too_large; so an announcement writer refuses a unit whose
// ANNOUNCED vocabulary ([MessageVocabulary], the entries Announce writes) has
// more entries than that, and writes the one with exactly 4096.
//
// AnnounceSchema(entries) is the schema every port's announcement test shares:
// a FIXED root (only fixed tables ride the message form) with an enum K whose
// variants fill the vocabulary and `fixed table Root { keys [K]uint8 }`. Its
// announced vocabulary has exactly `entries` entries: the variants and the
// field `keys`.
const AnnounceOverhead = 1

func AnnounceSchema(entries int) string {
	var b strings.Builder
	b.WriteString("package idcap\n\nenum K {\n")
	for i := 0; i < entries-AnnounceOverhead; i++ {
		fmt.Fprintf(&b, "    V%d\n", i)
	}
	b.WriteString("}\n\nfixed table Root {\n    keys [K]uint8\n}\n")
	return b.String()
}
