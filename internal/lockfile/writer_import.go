package lockfile

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ImportWriter attaches an explicitly supplied historical declaration to one
// existing lineage entry. The caller owns the provenance of that source:
// legacy hashes omit some facts, so matching a hash cannot authenticate it.
// No wire fact, entry order, retired mark or reason is changed. Existing
// different captured constraints are never overwritten.
func ImportWriter(u *ir.Unit, paths []string, target string, writer *ir.Unit) (path string, rewrote bool, err error) {
	name, hashText, hasHash := strings.Cut(target, "@")
	if !hasHash || name == "" {
		return "", false, fmt.Errorf("--writer names one locked layout as Table@0x<hash>")
	}
	hash, err := parseHex("wire="+hashText, "wire")
	if err != nil {
		return "", false, err
	}
	path, ok, err := Locate(paths)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, fmt.Errorf("this unit has no %s to enrich", FileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	locked, err := Parse(path, data)
	if err != nil {
		return "", false, err
	}
	if locked.Package != u.Package || writer == nil || writer.Package != u.Package {
		return "", false, fmt.Errorf("historical writer and lock must belong to package %s", u.Package)
	}
	live := Render(u)
	salvageLineage(locked, live)
	if errs := Diff(locked, live, Current); len(errs) > 0 {
		return "", false, fmt.Errorf("%s: bring the lock current with schema lock before importing writer metadata: %w", path, errs[0])
	}
	table := locked.Table(name)
	if table == nil || table.Decl != DeclFixedTable {
		return "", false, fmt.Errorf("this lock carries no fixed table %s", name)
	}
	var source *ir.Struct
	for _, st := range ir.TableFixedRoots(writer) {
		if st.WireName() == name {
			source = st
			break
		}
	}
	if source == nil || !ir.TableFixedEmitted(writer, source) {
		return "", false, fmt.Errorf("historical source has no admitted fixed-form root %s", name)
	}
	at := -1
	for i := range table.Lineage {
		if table.Lineage[i].Wire == hash {
			at = i
			break
		}
	}
	if at < 0 {
		return "", false, fmt.Errorf("fixed table %s has no lineage wire=0x%016x", name, hash)
	}
	old := table.Lineage[at]
	candidate := old
	candidate.Writer = source
	if err := validateWriter(candidate, name); err != nil {
		return "", false, err
	}
	if old.Writer != nil && !sameWriterConstraints(old.Writer, source) {
		return "", false, fmt.Errorf("fixed table %s, lineage wire=0x%016x: distinct captured writer constraints share this legacy hash; the existing trusted snapshot is not overwritten", name, hash)
	}
	if old.Writer == nil {
		table.Lineage[at].Writer = source
	}
	locked.Version = Version
	next := locked.Text()
	if next == string(data) {
		return path, false, nil
	}
	if err := os.WriteFile(path, []byte(next), 0644); err != nil {
		return "", false, err
	}
	return path, true, nil
}
