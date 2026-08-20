package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/internal/pack"
	"github.com/mas-bandwidth/schema/ir"
)

// A PackOutput is one binary the data compiler produced — the transition form
// of the table layer, one manifest output.
type PackOutput struct {
	// File is where the manifest asked for the bytes, resolved against the
	// manifest's own directory. Nothing is written there: like Generate, Pack
	// hands back bytes and the caller decides.
	File string
	// Bytes is the container: magic, the unit's protocol id, the content hash,
	// then the collections in manifest order.
	Bytes []byte
	// ContentHash is the fnv64a over everything after the container header —
	// the value carried inside Bytes, so a caller can report it without
	// re-deriving the header layout. Equal protocol id and equal content hash
	// mean equal data.
	ContentHash uint64
}

// Pack runs the data compiler over a manifest: it loads the unit the manifest
// names (through this Compiler, so FormatInPlace and OnFormat apply as they do
// everywhere else), encodes each declared collection's JSON instances on the
// table wire, and returns the unit alongside one [PackOutput] per manifest
// output. Paths inside the manifest resolve against the manifest's own
// directory.
func (c *Compiler) Pack(manifestPath string) (*ir.Unit, []PackOutput, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	var m pack.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	baseDir := filepath.Dir(manifestPath)
	paths, err := GatherPaths([]string{filepath.Join(baseDir, m.Unit)})
	if err != nil {
		return nil, nil, err
	}
	unit, err := c.Load(paths)
	if err != nil {
		return nil, nil, err
	}
	enc := &pack.Encoder{Unit: unit}
	outputs := make([]PackOutput, 0, len(m.Outputs))
	for _, out := range m.Outputs {
		bin, err := pack.BuildOutput(unit, enc, out, baseDir)
		if err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, PackOutput{
			File:        filepath.Join(baseDir, out.File),
			Bytes:       bin,
			ContentHash: pack.Fnv64a(bin[packHeaderBytes:]),
		})
	}
	return unit, outputs, nil
}

// packHeaderBytes is the container header the content hash is taken after:
// magic (4) + protocol id (8) + the hash field itself (8).
const packHeaderBytes = 20
