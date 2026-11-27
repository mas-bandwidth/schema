"""The write-refusal-shape surface (DW-TABLE-SHAPE; FORMAT.md,
`write-refusal-shape`): a value of the WRONG KIND put where the row's path
names, then the root's own writer, Measure and Save.

Python refuses in every build: a write refusal is the writer's ValueError.
Any other exception is no refusal: it is answered `raised`, which is red
against either verdict. Python's values hold every token but one: there is no
improper list, so `improper` is `unrepresentable`. The lawful twins (an
ordinary kind) are set and written exactly as write-refusal writes them.
"""

import importlib.util
import os

from _common import answering, entries, no_codec, root_class, supported, write_bytes
from _mutate import UNREPRESENTABLE, fresh_or_load, split

_spec = importlib.util.spec_from_file_location("_wref_plain", os.path.join(os.path.dirname(__file__), "write-refusal.py"))
_plain = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_plain)

# the closed token set (manifest.go writeShapeTokens), each as Python spells it
_TOKENS = {
    "int": lambda: 1,
    "float": lambda: 1.5,
    "numtext": lambda: "1.5",
    "text": lambda: "x",
    "bool": lambda: True,
    "null": lambda: None,
    "map": lambda: {},
    "seq": lambda: [1],
}


def _place(value, path, wrong):
    """Put wrong where path names and return the whole value to write. `f[i]`
    makes the list f its first i elements as loaded, then wrong; a last part
    naming an arm of a union selects that arm with wrong as its payload."""
    if path == ".":
        return wrong
    parts = split(path)
    node = value
    for i, part in enumerate(parts[:-1]):
        if parts[i + 1].isdigit() and i + 1 == len(parts) - 1:
            break
        node = node._values[int(part)] if part.isdigit() and hasattr(node, "_values") else node[int(part)] if part.isdigit() else getattr(node, part)
    last = parts[-1]
    if last.isdigit():
        name = parts[-2]
        items = list(getattr(node, name))[: int(last)]
        items.append(wrong)
        setattr(node, name, items)
        return value
    descriptor = getattr(type(node), "descriptor", None)
    for arm in getattr(descriptor, "arms", ()) or ():
        if arm.name == last:
            node.type = arm.tag
    setattr(node, last, wrong)
    return value


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal-shape"):
        with answering(row[1]):
            case, unit, root, wire, path, rule, token = row[1], row[2], row[3], row[4], row[5], row[6], row[7]
            cls = root_class(unit, root)
            if cls is None or not supported(cls):
                no_codec(outdir, case)
                continue
            value = fresh_or_load(cls, wire)
            if rule != "shape":
                if _plain._set(value, path, rule, token) is UNREPRESENTABLE:
                    write_bytes(outdir, case, b"unrepresentable\n")
                    continue
            elif token not in _TOKENS:
                write_bytes(outdir, case, b"unrepresentable\n")
                continue
            else:
                value = _place(value, path, _TOKENS[token]())
            try:
                cls.measure(value)
                cls.save(value)
                answer = b"wrote\n"
            except ValueError:
                answer = b"refused\n"
            except Exception:  # noqa: BLE001 - any other raise is no refusal, and red
                answer = b"raised\n"
            write_bytes(outdir, case, answer)
