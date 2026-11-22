package main

import (
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
	"github.com/mas-bandwidth/schema/v2/internal/format"
)

// formatSources is `schema fmt` (or `fmt --migrate`) over every path as ONE
// request (docs/CLI.md): every source is read and formatted before any is
// written, and every one that changes is published together through one
// transaction of the publisher. A source that does not parse, or a write
// that fails, leaves every source as it was, the ones before it in the
// directory included; it used to publish each file as it went, so an error
// in the second left the first rewritten. It answers the paths it rewrote,
// in the order given; f is the publisher's seam.
func formatSources(f atomicfile.Files, paths []string, migrate bool) ([]string, error) {
	txn := atomicfile.New()
	defer txn.Close()
	v := txn.With(f)
	var rewrote []string
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var out []byte
		if migrate {
			out, err = format.Migrate(p, data)
		} else {
			out, err = format.Format(p, data)
		}
		if err != nil {
			return nil, err
		}
		if string(out) == string(data) {
			continue
		}
		v.Write(p, out, 0o644)
		rewrote = append(rewrote, p)
	}
	if len(rewrote) == 0 {
		return nil, nil
	}
	if err := txn.Commit(); err != nil {
		return nil, err
	}
	return rewrote, nil
}
