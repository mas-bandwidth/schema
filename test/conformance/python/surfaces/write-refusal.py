"""The write-refusal surface: set one field, then Measure and Save."""

from copy import deepcopy

from _common import absent, entries, root_class, supported, write_bytes
from _mutate import UNREPRESENTABLE, apply, fresh_or_load


def _convert_scalar(current, rule, token):
    if rule == "length":
        length = int(token)
        if length < 0:
            raise ValueError("length cannot be represented by a negative Python length")
        return ("a" if isinstance(current, str) else b"a") * length
    if rule in ("ordinal", "int"):
        return int(token)
    if rule == "bool":
        return token.lower() in ("true", "1")
    if rule == "float":
        return float(token)
    if rule == "bytes":
        raw = bytes.fromhex(token)
        return raw.decode("utf-8", "surrogateescape") if isinstance(current, str) else raw
    raise ValueError("unknown write-refusal mutation: " + rule)


def _set(value, path, rule, token):
    """Set the row's value. Returns UNREPRESENTABLE when this leg's storage
    cannot hold it (_mutate.apply), and None once it is set."""
    special = apply(value, path, rule, token)
    if special is UNREPRESENTABLE:
        return UNREPRESENTABLE
    if special:
        return None
    parts = path.replace("[", ".").replace("]", "").split(".")
    node = value
    i = 0
    while i < len(parts) - 1:
        part = parts[i]
        if part.isdigit():
            idx = int(part)
            if isinstance(node, dict):
                keys = sorted(node.keys())
                old_k = keys[idx]
                old_v = node[old_k]
                which = parts[i + 1]
                assert i + 1 == len(parts) - 1
                if which == "key":
                    new_k = _convert_scalar(old_k, rule, token)
                    del node[old_k]
                    node[new_k] = old_v
                elif which == "value":
                    node[old_k] = _convert_scalar(old_v, rule, token)
                else:
                    raise KeyError(which)
                return
            if hasattr(node, "_values"):
                node = node._values[idx]
            else:
                node = node[idx]
        else:
            node = getattr(node, part)
        i += 1
    name = parts[-1]
    if name.isdigit():
        idx = int(name)
        slots = node._values if hasattr(node, "_values") else node
        current = slots[idx]
        slots[idx] = _convert_scalar(current, rule, token)
        return
    current = getattr(node, name)
    if rule == "count":
        count = int(token)
        if count < 0 or not isinstance(current, list) or (count > 0 and not current):
            raise ValueError("count mutation needs a nonnegative count and a populated list")
        resized = current[:count]
        resized.extend(deepcopy(current[0]) for _ in range(count - len(resized)))
        setattr(node, name, resized)
    elif rule == "tag":
        union = type(current)()
        union.type = int(token)
        setattr(node, name, union)
    else:
        setattr(node, name, _convert_scalar(current, rule, token))


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal"):
        case, unit, root, wire, path, rule, token = row[1], row[2], row[3], row[4], row[5], row[6], row[7]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        if not supported(cls):
            absent(outdir, case, "the python table runtime does not carry this construct")
            continue
        value = fresh_or_load(cls, wire)
        if _set(value, path, rule, token) is UNREPRESENTABLE:
            write_bytes(outdir, case, b"unrepresentable\n")
            continue
        try:
            value.measure()
            value.save()
            write_bytes(outdir, case, b"wrote\n")
        except ValueError:
            write_bytes(outdir, case, b"refused\n")
