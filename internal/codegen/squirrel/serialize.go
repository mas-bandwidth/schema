package squirrel

import (
	"embed"
	"io/fs"
	"path"
)

// The vendored serialize port (schema-squirrel-serialize-vendor; docs/PORT-SQUIRREL.md,
// THE RUNTIME LAYOUT) is the type wire's bottom layer: the type-wire cards call this
// library rather than a private bit reader or writer. The port's src files live here
// under their OWN names — serialize.nut, serialize_bits.nut, ... — copied verbatim from
// the repository SERIALIZE_PIN names, and serial emitted beside the generated modules
// under those names, where schema's loader (schema_module.nut) resolves them (serialize.nut
// is run by schema's loader as a module with (exports, require) in vargv, which the port's
// design allows; its closed loader resolves the rest by their basenames beside it). The
// test TestVendoredSquirrelSerializeIsThePin pins the files against SERIALIZE_PIN.
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

// init joins through RegisterSurface, the join point THE RUNTIME LAYOUT gives later files
// (docs/PORT-SQUIRREL.md, THE CARD MAP): read.go and write.go join the same way, and the
// serialize library is the bottom layer their emitted code calls.
func init() {
	RegisterSurface(Surface{
		Name: "serialize",
	})
}
