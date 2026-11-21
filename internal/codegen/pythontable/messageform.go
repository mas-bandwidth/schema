// Message-form constants are settled from the same vocabulary as the table
// engine (docs/SPEC-TABLES.md §3.3), not reconstructed from Python values.
package pythontable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// messageModule returns an empty string when the unit has no declared fixed
// table: variable tables never acquire a message-form entry point.
func messageModule(u *ir.Unit) string {
	roots := make([]string, 0)
	for _, st := range u.Tables {
		if st.FixedDeclared {
			roots = append(roots, st.Name)
		}
	}
	if len(roots) == 0 {
		return ""
	}
	sort.Strings(roots)
	var b strings.Builder
	b.WriteString("\"\"\"The unit's message-form announcement and entry facts.\"\"\"\n\n")
	fmt.Fprintf(&b, "ANNOUNCEMENT = bytes.fromhex(%q)\n", fmt.Sprintf("%x", tablewire.Announce(u)))
	fmt.Fprintf(&b, "BUILD_VERSION = %d\n", ir.BuildVersion(u))
	b.WriteString("ROOTS = frozenset((\n")
	for _, name := range roots {
		fmt.Fprintf(&b, "    %q,\n", name)
	}
	b.WriteString("))\n_CRC_ROOTS: frozenset[str] = frozenset((\n")
	for _, name := range roots {
		if u.Tables[name].Integrity == "crc32c" {
			fmt.Fprintf(&b, "    %q,\n", name)
		}
	}
	b.WriteString("))\nFIELD_KEYS: dict[tuple[str, str], bytes] = {\n")
	closure := tablewire.MessageClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			entry := ir.TableFieldEntry(f)
			fmt.Fprintf(&b, "    (%q, %q): bytes.fromhex(%q),\n", st.Name, f.Name, fmt.Sprintf("%x", entry.Encode(nil)))
		}
	}
	unions := closureUnions(u)
	unionNames := make([]string, 0, len(unions))
	for name := range unions {
		unionNames = append(unionNames, name)
	}
	sort.Strings(unionNames)
	for _, name := range unionNames {
		un := u.TableUnions[name]
		if un == nil {
			un = u.Unions[name]
		}
		if un == nil {
			continue
		}
		for _, variant := range un.Variants {
			entry := ir.TableArmEntry(variant)
			fmt.Fprintf(&b, "    (%q, %q): bytes.fromhex(%q),\n", un.Name, variant.Name, fmt.Sprintf("%x", entry.Encode(nil)))
		}
	}
	b.WriteString("}\n")
	b.WriteString("_RECORD_NAMES = (\n")
	for _, name := range names {
		fmt.Fprintf(&b, "    %q,\n", name)
	}
	b.WriteString(")\n")
	b.WriteString(`
from typing import Any, Callable, TypeVar, cast
from . import LoadReport
from .message import BuildVersionMismatch as BuildVersionMismatch, MessageVocabulary as _Vocabulary, announce_read as _announce, resolve_vocabulary as _resolve
from .message import BitWriter, _write_value
from .descriptor import by_name as _by_name
from .integrity import open_crc as _open_crc, seal_crc as _seal_crc
from .message import load_messages as _load, measure_messages as _measure, save_messages as _save

_T = TypeVar("_T")

class MessageVocabulary(_Vocabulary):
    def __init__(self, strict: bool = False) -> None:
        super().__init__(BUILD_VERSION, strict)

_local = MessageVocabulary()
_announce(_local, ANNOUNCEMENT)
_records: dict[str, type[Any]] = {}
for _name in _RECORD_NAMES:
    _record = _by_name(_name)
    if _record is not None:
        _records[_name] = _record

def announce_read(vocab: MessageVocabulary, data: bytes | bytearray | memoryview, *, max_bytes: int = 65536, max_entries: int = 4096) -> None:
    _announce(vocab, data, max_bytes=max_bytes, max_entries=max_entries)
    _resolve(vocab, _local, _records)

def save_messages(root: type[_T], values: list[_T]) -> bytes:
    raw = _save(root, list(values), _local, FIELD_KEYS, ROOTS, _WRITERS.get(getattr(getattr(root, "descriptor", None), "name", "")))
    return _seal_crc(raw) if getattr(getattr(root, "descriptor", None), "name", "") in _CRC_ROOTS else raw

def measure_messages(root: type[_T], values: list[_T]) -> int:
    size = _measure(root, list(values), _local, FIELD_KEYS, ROOTS, _WRITERS.get(getattr(getattr(root, "descriptor", None), "name", "")))
    return size + (4 if getattr(getattr(root, "descriptor", None), "name", "") in _CRC_ROOTS else 0)

def load_messages(root: type[_T], vocab: MessageVocabulary, data: bytes | bytearray | memoryview, capacity: int = 256, report: LoadReport | None = None) -> list[_T]:
    if getattr(getattr(root, "descriptor", None), "name", "") in _CRC_ROOTS:
        data = _open_crc(data, 2)
    return cast(list[_T], _load(root, vocab, data, BUILD_VERSION, FIELD_KEYS, ROOTS, capacity, report))
`)
	entries := tablewire.MessageVocabulary(u)
	slots := make(map[string]int, len(entries))
	for i, entry := range entries {
		slots[entry.Key()] = i + 1
	}
	b.WriteString("\n_WRITERS: dict[str, Callable[[BitWriter, Any], None]] = {}\n")
	for index, name := range roots {
		st := u.Tables[name]
		valid := true
		for _, field := range st.Fields {
			if slots[ir.TableFieldEntry(field).Key()] == 0 {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		fmt.Fprintf(&b, "_class_%d = _by_name(%q)\nassert _class_%d is not None\n_fields_%d = _class_%d.descriptor.fields\n", index, name, index, index, index)
		fmt.Fprintf(&b, "def _writer_%d(writer: BitWriter, value: Any) -> None:\n", index)
		for i, field := range st.Fields {
			slot := slots[ir.TableFieldEntry(field).Key()]
			fmt.Fprintf(&b, "    field = _fields_%d[%d]\n    current = getattr(value, %q)\n", index, i, field.Name)
			fmt.Fprintf(&b, "    if not field.is_default(current) or getattr(field, 'required', False):\n        writer.put(%d, _local.ref_bits)\n        _write_value(writer, field, current, _local.entries[%d].shape, _local, FIELD_KEYS)\n", slot, slot-1)
		}
		fmt.Fprintf(&b, "    writer.put(0, _local.ref_bits)\n_WRITERS[%q] = _writer_%d\n", name, index)
	}
	return b.String()
}
