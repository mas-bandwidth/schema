// Message-form constants are settled from the same vocabulary as the table
// engine (docs/SPEC-TABLES.md §3.3), not reconstructed from Python values.
package pythontable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
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
	b.WriteString("}\n")
	b.WriteString(`
from typing import TypeVar, cast
from ._schema import LoadReport
from ._schema.message import MessageVocabulary as _Vocabulary, announce_read as _announce, resolve_vocabulary as _resolve
from ._schema.message import load_messages as _load, save_messages as _save

_T = TypeVar("_T")

class MessageVocabulary(_Vocabulary):
    def __init__(self, strict: bool = False) -> None:
        super().__init__(BUILD_VERSION, strict)

_local = MessageVocabulary()
_announce(_local, ANNOUNCEMENT)

def announce_read(vocab: MessageVocabulary, data: bytes, *, max_bytes: int = 65536, max_entries: int = 4096) -> None:
    _announce(vocab, data, max_bytes=max_bytes, max_entries=max_entries)
    _resolve(vocab, _local)

def save_messages(root: type[_T], values: list[_T]) -> bytes:
    return _save(root, list(values), _local, FIELD_KEYS, ROOTS)

def measure_messages(root: type[_T], values: list[_T]) -> int:
    return len(save_messages(root, values))

def load_messages(root: type[_T], vocab: MessageVocabulary, data: bytes, capacity: int = 256, report: LoadReport | None = None) -> list[_T]:
    return cast(list[_T], _load(root, vocab, data, BUILD_VERSION, FIELD_KEYS, ROOTS, capacity, report))
`)
	return b.String()
}
