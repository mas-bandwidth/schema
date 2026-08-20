// Table-wire field identity (notes/table-wire.md): target-independent, so
// the six backends, the data compiler and any generator outside this module
// all derive one id for one field name.
package ir

import "fmt"

// FieldId is the stable wire identity of a field: fold16(fnv1a32(name)),
// rebounding 0 (the terminator) to 1.
func FieldId(name string) uint16 {
	h := uint32(0x811C9DC5)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 0x01000193
	}
	id := uint16((h ^ (h >> 16)) & 0xFFFF)
	if id == 0 {
		id = 1
	}
	return id
}

// CheckTableIds refuses field-id collisions per type — loud, at compile time.
func CheckTableIds(u *Unit) error {
	for name, st := range u.Structs {
		seen := map[uint16]string{}
		for _, f := range st.Fields {
			id := FieldId(f.Name)
			if prev, dup := seen[id]; dup {
				return fmt.Errorf("type %s: fields %s and %s collide on table-wire id 0x%04x — rename one (notes/table-wire.md)", name, prev, f.Name, id)
			}
			seen[id] = f.Name
		}
	}
	return nil
}
