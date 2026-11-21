"""The write-refusal surface: set one field, then Measure and Save."""

import sys
from copy import deepcopy

from _common import answering, no_codec, entries, read_bytes, root_class, supported, write_bytes


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


_ELEMENT_DEFAULTS = {"bool": False, "float32": 0.0, "float64": 0.0, "string": "", "bytes": b"", "int": 0, "enum": 0}


def _element_default(node, name):
    """The default of one element of the list field name on node, by the
    field's declared element kind (its descriptor)."""
    descriptor = getattr(type(node), "descriptor", None)
    field = next((f for f in descriptor.fields if f.name == name), None) if descriptor is not None else None
    if field is None:
        raise ValueError("count mutation: " + name + " has no field descriptor")
    if field.target:
        module = type(node).__module__
        for owner in (sys.modules.get(module), sys.modules.get(module.split(".")[0])):
            cls = getattr(owner, field.target, None)
            if cls is not None:
                return cls()
    if field.elem_codec in _ELEMENT_DEFAULTS:
        return _ELEMENT_DEFAULTS[field.elem_codec]
    raise ValueError("count mutation: no element default for " + name + " (" + field.elem_codec + ")")


def _set(value, path, rule, token):
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
        if count < 0 or not isinstance(current, list):
            raise ValueError("count mutation needs a nonnegative count and a list")
        # an empty list grows by its DECLARED element kind's default, never by
        # copying an element it does not have
        seed = current[0] if current else (_element_default(node, name) if count > 0 else None)
        resized = current[:count]
        resized.extend(deepcopy(seed) for _ in range(count - len(resized)))
        setattr(node, name, resized)
    elif rule == "tag":
        union = type(current)()
        union.type = int(token)
        setattr(node, name, union)
    else:
        setattr(node, name, _convert_scalar(current, rule, token))


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal"):
        with answering(row[1]):
            case, unit, root, wire, path, rule, token = row[1], row[2], row[3], row[4], row[5], row[6], row[7]
            cls = root_class(unit, root)
            if cls is None:
                no_codec(outdir, case)
                continue
            if not supported(cls):
                no_codec(outdir, case)
                continue
            value = cls.load(read_bytes(wire))
            _set(value, path, rule, token)
            try:
                value.measure()
                value.save()
                write_bytes(outdir, case, b"wrote\n")
            except ValueError:
                write_bytes(outdir, case, b"refused\n")
