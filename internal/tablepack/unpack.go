// `schema unpack` — the inverse (docs/SPEC-TABLES.md §17.3): a root table's wire
// bytes become the tree again, through §16's text form, which is the tool round
// trip §1 promises. `unpack` -> `pack` is byte-stable.
package tablepack

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Unpack decodes a root table's wire bytes and writes the tree under dir. The
// report is the wire's own (§4): silence means the bytes matched this schema
// exactly.
//
// §17.2 permits a field's value to live in a `<field>.json` or in a directory,
// and this writer takes the EXPANDED form: one `<field>.json` per field, and
// one `<field>/<Variant>.json` per slot of an enum-keyed array — the shape a
// person edits, and the one that exercises the directory rule rather than the
// one-file shortcut. An absent `?T` and a guarded-out field write no file at
// all, because omission is how a tree says absence.
//
// It also PRUNES, and it prunes for THE ROOT'S WHOLE SHAPE rather than for the
// shape it happens to be writing: every `<field>.json`, every `<field>/`
// directory AND the `<Root>.json` that names the root itself is removed unless
// this run wrote it. Both halves matter. Without the first, unpacking a newer
// `.bin` over yesterday's tree leaves the file an absent optional used to have
// standing beside the new one. Without the second, switching between the
// expanded shape and `--one-file` in one directory leaves BOTH — and `pack`
// refuses that tree by name ("a root is one file or one tree of fields, never
// both"), so the tool would be writing a tree its own sibling verb rejects.
//
// An entry that names NO part of the root is left exactly where it is: it is
// not this tool's, and `pack` names it if it does not belong.
//
// A VARIABLE root is written as ONE `<Root>.json` whichever shape is asked
// for (§17.2): its shared nodes are named by labels a text owns (§16.7), and a
// tree of fields would split them across texts that cannot name each other's
// nodes — which is the tree `pack` refuses for the same reason.
func Unpack(m *tabletext.Model, root string, wire []byte, dir string) (tabletext.Report, error) {
	return unpack(m, root, wire, dir, false)
}

// UnpackOneFile writes the root as ONE `<Root>.json` instead of a tree of
// fields — §17.2's last rule as an output shape. It is the same instance and
// the same writer, so it packs to the same bytes the expanded tree does; it
// exists because one text of the whole root is what a backend's `ToJson`
// produces, and comparing the two is §17.1's third golden.
func UnpackOneFile(m *tabletext.Model, root string, wire []byte, dir string) (tabletext.Report, error) {
	return unpack(m, root, wire, dir, true)
}

// ReadReport decodes one wire as one root and answers the §4 report, writing
// nothing.
func ReadReport(m *tabletext.Model, root string, wire []byte) (tabletext.Report, error) {
	st := m.Unit.Tables[root]
	if st == nil {
		return tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", root, strings.Join(m.Roots(), ", "))
	}
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(st), wire, &report)
	if tablewire.Refused(err) {
		// THE REFUSAL IS THE ANSWER, not a failure to produce one: a form this
		// reader does not carry is a verdict the report states, and nothing was
		// decoded to report events over (docs/SPEC-TABLES.md §3)
		return report, nil
	}
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("the bytes are not a %s body — the framing is damaged past the point the walk could continue (docs/SPEC-TABLES.md §4)", root)
	}
	return report, nil
}

// UnpackMessages reads a BATCH against the announcement that carried it. The
// announcement is an ordinary form 1 file and is read first, tolerantly, with
// its two strict checks; a refused announcement sets no table, so the batch is
// refused for want of one and nothing is written.
//
// The caller names one root and one output tree per message, in the batch's own
// order, because which root a message is, is not on this wire.
func UnpackMessages(m *tabletext.Model, trees []MessageTree, announcement, message []byte, oneFile bool) (tabletext.Report, error) {
	var report tabletext.Report
	var vocabulary tablewire.Vocabulary
	if err := vocabulary.AnnounceRead(announcement, &report); err != nil {
		return report, err
	}
	if !vocabulary.Announced() {
		return report, fmt.Errorf("the announcement is malformed and set no table (docs/SPEC-TABLES.md §3, §3.3)")
	}
	count, err := tablewire.MessageCount(message, &vocabulary)
	if err != nil {
		return report, err
	}
	if count != len(trees) {
		return report, fmt.Errorf("the batch carries %d messages and %d root were named: which root each message is, is the application's and not this wire's (docs/SPEC-TABLES.md §3.3)", count, len(trees))
	}
	insts := make([]*tabletext.Instance, 0, len(trees))
	for _, tree := range trees {
		st := m.Unit.Tables[tree.Root]
		if st == nil {
			return report, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", tree.Root, strings.Join(m.Roots(), ", "))
		}
		// THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §2.2,
		// §3.3), and the class is declared: a variable table has no message
		// form, and a message-form request naming one is refused naming the
		// table rather than quietly writing another form.
		if !st.FixedDeclared {
			return report, fmt.Errorf("--root %s names a variable table, and the MESSAGE FORM is a fixed table's: a message is a bitpacked body under one announced vocabulary, so the shape has to be one the declaration fixes. Declare it `fixed table %s`, or use the FILE form, which every table has (docs/SPEC-TABLES.md §2.2, §3.3)", tree.Root, tree.Root)
		}
		insts = append(insts, m.New(st))
	}
	// the refusal comes back BEFORE anything is written: a batch this engine
	// does not decode is not a tree half-written to disk
	delivered, ok, err := tablewire.DecodeMessages(m, insts, message, &vocabulary, &report)
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("the bytes are not a batch of these roots: the framing is damaged inside body %d, and the %d before it stand (docs/SPEC-TABLES.md §3.3, §4)", delivered+1, delivered)
	}
	// a batch is one publish: every tree lands, or none does
	txn := atomicfile.New()
	defer txn.Close()
	for i, tree := range trees {
		if err := writeTree(txn, m, insts[i], tree.Root, tree.Dir, oneFile || m.NeedsLabels(tree.Root)); err != nil {
			return report, err
		}
	}
	return report, txn.Commit()
}

func unpack(m *tabletext.Model, root string, wire []byte, dir string, oneFile bool) (tabletext.Report, error) {
	return unpackWith(m, root, wire, dir, oneFile, nil)
}

// unpackWith is both forms' one walk: a nil vocabulary reads the VARIABLE form,
// whose id table is its own trailer, and a non-nil one reads the MESSAGE form
// against the connection's (docs/SPEC-TABLES.md §3, §3.3). Nothing else about
// the tree, the text or the report differs.
func unpackWith(m *tabletext.Model, root string, wire []byte, dir string, oneFile bool, vocabulary *tablewire.Vocabulary) (tabletext.Report, error) {
	st := m.Unit.Tables[root]
	if st == nil {
		return tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", root, strings.Join(m.Roots(), ", "))
	}
	oneFile = oneFile || m.NeedsLabels(root)
	inst := m.New(st)
	var report tabletext.Report
	// the refusal comes back BEFORE anything is written: a root this engine
	// does not decode is not a tree half-written to disk
	decode := func() (bool, error) { return tablewire.Decode(m, inst, wire, &report) }
	if vocabulary != nil {
		decode = func() (bool, error) { return tablewire.DecodeMessage(m, inst, wire, vocabulary, &report) }
	}
	ok, err := decode()
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("the bytes are not a %s body — the framing is damaged past the point the walk could continue (docs/SPEC-TABLES.md §4)", root)
	}
	txn := atomicfile.New()
	defer txn.Close()
	if err := writeTree(txn, m, inst, root, dir, oneFile); err != nil {
		return report, err
	}
	return report, txn.Commit()
}

// writeTree is the half after the decode: one instance onto disk, in the shape
// §17.2 asks for. Both forms and both shapes land here, because the tree a
// message writes is the tree a file writes.
//
// It changes nothing itself. Every file it writes, every entry it prunes and
// every directory it respells is a change of txn, and the caller's Commit
// publishes them together (internal/atomicfile): a write that fails leaves
// the tree that was there, whole. What it reads of the directory is
// therefore the tree as it stood before this unpack.
func writeTree(txn *atomicfile.Txn, m *tabletext.Model, inst *tabletext.Instance, root, dir string, oneFile bool) error {
	st := inst.Def
	if !oneFile {
		// a key that is no safe path component at all is refused by name, as
		// before: the caller asked for a tree and is told how to keep the key
		for _, f := range st.Fields {
			key := ir.TableFieldJsonKey(f)
			if !treeComponent(key) {
				return fmt.Errorf("field %s: JSON key %q is not one safe local path component; use --one-file to preserve this key in JSON", f.Name, key)
			}
		}
	}
	if !oneFile && treeNamesOverlap(st, root) {
		// two entries of the expanded tree would share one name, or one would
		// take `<Root>.json`, which pack reads as the whole root (§17.2), or a
		// name is one a filesystem may store as another: that tree could not
		// be written whole or would not pack back. The whole root is the same
		// instance, so it is written instead, decided before anything touches
		// the disk.
		oneFile = true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	view, err := txn.OpenRoot(dir)
	if err != nil {
		return err
	}
	out := view.Root() // read through; the transaction closes it
	// what the ROOT's shape owns at this level, whichever shape is written:
	// every field key, and the root's own name
	owned := map[string]bool{root: true}
	for i := range st.Fields {
		owned[ir.TableFieldJsonKey(st.Fields[i])] = true
	}
	// an entry in the way that names no field refuses the unpack before a
	// byte is written, and stays where it is (§17.3)
	if err := preflight(out, dir, st, root, oneFile, owned); err != nil {
		return err
	}
	if oneFile {
		text, err := m.Write(inst)
		if err != nil {
			return err
		}
		name := root + ".json"
		if _, err := claimEntry(view, out, dir, name, false, owned); err != nil {
			return err
		}
		view.Write(name, append(text, '\n'), 0o644)
		return prune(view, out, owned, map[string]bool{name: true})
	}
	guards := tabletext.Guards(st)
	written := map[string]bool{}
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		key := ir.TableFieldJsonKey(f)
		if terms, guarded := guards[f.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if f.Type.Optional && !fv.Present {
			continue
		}
		if f.KeyEnum != "" {
			if err := unpackKeyed(m, fv, view, out, dir, key, owned); err != nil {
				return err
			}
			written[key] = true
			continue
		}
		text, err := m.WriteValue(fv)
		if err != nil {
			return err
		}
		if _, err := claimEntry(view, out, dir, key+".json", false, owned); err != nil {
			return err
		}
		view.Write(key+".json", append(text, '\n'), 0o644)
		written[key+".json"] = true
	}
	return prune(view, out, owned, written)
}

// treeNamesOverlap reports whether the expanded tree's entries at the root
// level collide: every field's entry (`<key>.json` for a value, `<key>/` for
// an enum-keyed array) is computed first, with `<Root>.json` reserved for the
// whole root, and any two that name one entry — compared case-folded, since
// a case-insensitive filesystem would merge them too — overlap. Every
// declared field counts, written or not, so the shape a schema unpacks to
// does not depend on the values.
//
// A name that is not plain portable ASCII counts as an overlap too: two
// different Unicode spellings (é as one code point, or e and a combining
// accent) are different keys but one path on a filesystem that normalizes
// names, as macOS does, and no rule short of the filesystem's own says which
// pairs those are. The whole root keeps such keys inside one JSON text, where
// they stay distinct. An enum-keyed array's variant files are checked the
// same way inside their directory.
func treeNamesOverlap(st *ir.Struct, root string) bool {
	taken := map[string]bool{}
	claim := func(seen map[string]bool, name string) bool {
		if !portableName(name) {
			return true
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return true
		}
		seen[folded] = true
		return false
	}
	if claim(taken, root+".json") {
		return true
	}
	for _, f := range st.Fields {
		name := ir.TableFieldJsonKey(f)
		if f.KeyEnum == "" {
			name += ".json"
		}
		if claim(taken, name) {
			return true
		}
		if f.KeyEnum == "" {
			continue
		}
		variants := map[string]bool{}
		for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
			if claim(variants, tabletext.EnumName(f.KeyEnumRef, tabletext.KeyedSlotValue(f, slot))+".json") {
				return true
			}
		}
	}
	return false
}

// portableName holds a name to the POSIX portable filename characters,
// [A-Za-z0-9._-]: the names every filesystem stores as written.
func portableName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func unpackKeyed(m *tabletext.Model, fv *tabletext.Field, parentView atomicfile.View, parent *os.Root, where, dir string, parentOwned map[string]bool) error {
	alias, err := claimEntry(parentView, parent, where, dir, true, parentOwned)
	if err != nil {
		return err
	}
	// the directory as it stands now, under the spelling it has: read for
	// what is in the way and what is stale. One that is not there yet holds
	// neither, and the commit makes it with its first file.
	at := dir
	if alias != "" {
		at = alias
	}
	var out *os.Root
	if _, err := parent.Lstat(at); err == nil {
		if out, err = parent.OpenRoot(at); err != nil {
			return err
		}
		defer func() { _ = out.Close() }() // only the directory handle, for reading
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	view := parentView.Under(dir)
	f := fv.Def
	written := map[string]bool{}
	owned := map[string]bool{}
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		name := tabletext.EnumName(f.KeyEnumRef, tabletext.KeyedSlotValue(f, slot))
		if name == "" {
			return fmt.Errorf("enum-keyed array %s: slot %d belongs to no variant of %s", f.Name, slot, f.KeyEnum)
		}
		owned[name] = true
		text, err := m.WriteElement(fv, slot)
		if err != nil {
			return err
		}
		if out != nil {
			if _, err := claimEntry(view, out, filepath.Join(where, dir), name+".json", false, owned); err != nil {
				return err
			}
		}
		view.Write(name+".json", append(text, '\n'), 0o644)
		written[name+".json"] = true
	}
	if out == nil {
		return nil
	}
	return prune(view, out, owned, written)
}

// prune removes the entries at one level that NAME something this level owns —
// a field key, or a variant of the keying enum — and that this unpack did not
// write. Both spellings of a value are pruned, so a stale `<field>/` directory
// left beside a fresh `<field>.json` cannot become "two entries claim one
// value" on the next pack. Anything the level does not own is left alone.
func prune(view atomicfile.View, out *os.Root, owned, written map[string]bool) error {
	entries, err := fs.ReadDir(out.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if written[e.Name()] {
			continue
		}
		// exactly what the entry names: claimEntry has already given every
		// written name its own spelling on a case-insensitive filesystem, and
		// an entry that only folds to an owned name names no field (§17.3)
		key, ok := entryKey(e)
		if !ok || !owned[key] {
			continue
		}
		view.Delete(e.Name())
	}
	return nil
}

// foldedIn reports whether name equals one of the written names under Unicode
// case folding, the comparison a case-insensitive filesystem makes. The names
// an unpack writes are portable ASCII (treeNamesOverlap), and normalization
// leaves ASCII as it is, so folding is the whole of the comparison.
func foldedIn(name string, written map[string]bool) bool {
	if written[name] {
		return true
	}
	for w := range written {
		if strings.EqualFold(name, w) {
			return true
		}
	}
	return false
}

// obstruction answers what stands where name, about to be written at this
// level as a file or (dir) a directory, must go. Nothing, or the exact entry
// of the right kind: "" and nil. An entry the filesystem takes for name but
// spells differently — a case-insensitive filesystem keeps the old spelling
// when a write lands on it — that names, under the folding that filesystem
// applies, a value this level owns: its name, for claimEntry to rename (a directory, children and all) or remove (a value file). Any
// other entry in the way names no field (§17.3) and is never deleted: the
// unpack is refused by naming it, where it lies.
func obstruction(out *os.Root, where, name string, dir bool, owned map[string]bool) (string, error) {
	refuse := func(entry string) error {
		return fmt.Errorf("%s: %s is in the way of the %s this unpack writes there, and it names no field the schema owns at that level, so it is left in place; move it and unpack again (docs/SPEC-TABLES.md §17.3)",
			where, entry, name)
	}
	entries, err := fs.ReadDir(out.FS(), ".")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == name {
			if e.IsDir() == dir {
				return "", nil
			}
			return "", refuse(e.Name())
		}
	}
	if _, err := out.Lstat(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil // nothing answers to the name on this filesystem
		}
		return "", err
	}
	// the name resolves to an entry spelled otherwise: the filesystem folds
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), name) {
			continue
		}
		stem, named := e.Name(), e.IsDir()
		if n := len(stem); !named && n > len(".json") && strings.EqualFold(stem[n-len(".json"):], ".json") {
			stem, named = stem[:n-len(".json")], true
		}
		if e.IsDir() == dir && named && foldedIn(stem, owned) {
			return e.Name(), nil
		}
		return "", refuse(e.Name())
	}
	return "", refuse(name)
}

// claimEntry makes name the one entry that answers to it before it is
// written: an owned entry the filesystem takes for it under another spelling
// is renamed to it (a directory, keeping every child) or removed (a value
// file), so the name lands spelled as written (pack matches names
// exactly); anything else in the way refuses the unpack and stays.
//
// The removal and the rename are changes of the unpack's transaction, made
// when it commits and undone if it fails. It answers the other spelling it
// found, "" for none.
func claimEntry(view atomicfile.View, out *os.Root, where, name string, dir bool, owned map[string]bool) (string, error) {
	alias, err := obstruction(out, where, name, dir, owned)
	if err != nil || alias == "" {
		return "", err
	}
	if !dir {
		// one owned value file, about to be written again
		view.Delete(alias)
		return alias, nil
	}
	// a directory may hold entries no schema owns (§17.3): it is renamed to
	// the written spelling with every child in it, never removed. Through a
	// temporary name (atomicfile's Respell), because a case-insensitive
	// filesystem may take a rename between two spellings of one name as a
	// rename onto itself.
	view.Respell(alias, name)
	return alias, nil
}

// preflight refuses, before anything is written, an entry that stands where
// the unpack would write and names no field: at the root's level (the whole
// root file, a field file, an enum-keyed array's directory) and inside an
// existing keyed directory (each variant file).
func preflight(out *os.Root, where string, st *ir.Struct, root string, oneFile bool, owned map[string]bool) error {
	if oneFile {
		_, err := obstruction(out, where, root+".json", false, owned)
		return err
	}
	for _, f := range st.Fields {
		key := ir.TableFieldJsonKey(f)
		if f.KeyEnum == "" {
			if _, err := obstruction(out, where, key+".json", false, owned); err != nil {
				return err
			}
			continue
		}
		alias, err := obstruction(out, where, key, true, owned)
		if err != nil {
			return err
		}
		at := key
		if alias != "" {
			at = alias
		}
		if fi, err := out.Lstat(at); err != nil || !fi.IsDir() {
			continue
		}
		sub, err := out.OpenRoot(at)
		if err != nil {
			return err
		}
		variants := keyedVariants(f)
		for _, name := range slices.Sorted(maps.Keys(variants)) {
			if _, err := obstruction(sub, filepath.Join(where, key), name+".json", false, variants); err != nil {
				_ = sub.Close()
				return err
			}
		}
		_ = sub.Close()
	}
	return nil
}

// keyedVariants is the variant names an enum-keyed array's directory owns.
func keyedVariants(f *ir.Field) map[string]bool {
	owned := map[string]bool{}
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		if name := tabletext.EnumName(f.KeyEnumRef, tabletext.KeyedSlotValue(f, slot)); name != "" {
			owned[name] = true
		}
	}
	return owned
}

// DescribeAnnouncement prints an announcement's VOCABULARY decoded
// (docs/SPEC-TABLES.md §3.3): one line an entry carrying the slot, the id,
// the name where this unit's own closure names it, the kind and the shape.
// An announcement is an ordinary form-1 file, and a tool that printed its
// two fields as opaque bytes and stopped would be hiding the only part
// anyone reads.
func DescribeAnnouncement(m *tabletext.Model, announcement []byte) (string, tabletext.Report, error) {
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(announcement, &report); err != nil {
		return "", report, err
	}
	if !vocabulary.Announced() {
		return "", report, fmt.Errorf("the announcement is malformed and set no vocabulary (docs/SPEC-TABLES.md §3, §3.3)")
	}
	names := map[uint64]string{}
	for _, e := range ir.TableVocabulary(m.Unit) {
		if name := ir.TableVocabularyName(m.Unit, e.Id); name != "" {
			names[e.Id] = name
		}
	}
	var b strings.Builder
	entries := vocabulary.Entries()
	fmt.Fprintf(&b, "build version %016x, %d entries, references %d bits\n", vocabulary.BuildVersion(), len(entries), vocabulary.RefBits())
	for i, e := range entries {
		name := names[e.Id]
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(&b, "%4d  %016x  %-24s  kind %-2d  %s\n", i+1, e.Id, name, e.Kind, ir.TableMessageShapeString(e.Kind, e.Shape))
	}
	return b.String(), report, nil
}

// JSON keys may name arbitrary text. Only the expanded filesystem form needs
// a safe local component; the one-file form keeps the original JSON vocabulary.
func treeComponent(key string) bool {
	return key != "" && key != "." && key != ".." && filepath.IsLocal(key) &&
		!strings.ContainsAny(key, "/\\:") && strings.IndexFunc(key, unicode.IsControl) < 0 &&
		!strings.HasSuffix(key, ".") && !strings.HasSuffix(key, " ")
}
