// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/javatable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// javaTarget emits Java 17.
type javaTarget struct{}

func (javaTarget) Names() []string { return []string{"java"} }

func (javaTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "java"); err != nil {
		return nil, err
	}
	files, err := java.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.java per file plus the
	// unit's shared runtime, one PUBLIC TYPE PER FILE — the TABLE-wire codecs,
	// FIXED class (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces
	tables, err := javatable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file, a table runtime type and a declaration's generated name all land in the Java package's one-public-class-per-file namespace; rename one of them (docs/SPEC-TABLES.md §11)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerBuiltin(javaTarget{}, true, false, false, false)
	registerPacketVoidArmCarrier("java")
}
