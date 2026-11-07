package squirrel

import (
	"embed"
	"io/fs"
	"path"
)

// The vendored serialize port (schema-squirrel-serialize-vendor; docs/PORT-SQUIRREL.md,
// THE RUNTIME LAYOUT) is the type wire's bottom layer: every Write<Name> and Read<Name>
// calls the serialize methods of the stream it is handed, and owns no bit reader or
// writer of its own. Generated modules never require it, as generated JavaScript never
// imports serialize.js: the caller builds the stream. The port's src files live here
// under their own names (serialize.nut, serialize_bits.nut, ...), copied verbatim from
// the repository SERIALIZE_PIN names, for the tests and harnesses that run the
// generated code; TestVendoredSquirrelSerializeIsThePin pins them against SERIALIZE_PIN.
//
//go:embed serialize/*.nut
var serializeFS embed.FS

// Serialize returns the vendored serialize port's files, keyed by the names they are
// emitted under beside the generated modules (serialize.nut, serialize_bits.nut, ...).
func Serialize() map[string][]byte {
	return SerializeFiles(serializeFS, "serialize")
}

// SerializeFiles reads every .nut under dir of fsys and keys it by basename.
func SerializeFiles(fsys fs.FS, dir string) map[string][]byte {
	out := map[string][]byte{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic(err) // the directory is embedded: a failure is a build defect
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != "."+ext {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}
