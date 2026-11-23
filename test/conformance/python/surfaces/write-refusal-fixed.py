"""The fixed writer declaration contract, over every manifest recipe."""

import importlib

from _common import absent, entries, root_class, write_bytes
from _mutate import UNREPRESENTABLE, apply, fresh_or_load


def _convert_fixed(field, previous, rule, token, default):
    if rule in ("int", "ordinal"):
        return int(token)
    if rule == "float":
        return float(token)
    if rule == "count":
        count = int(token)
        out = list(previous[:count])
        while len(out) < count:
            out.append(default(field["children"][0]))
        return out
    if rule == "length":
        count = int(token)
        return "x" * count if field["shape"] == "string" else bytes(count)
    if rule == "bytes":
        raw = bytes.fromhex(token)
        return raw.decode("utf-8", "surrogateescape") if field["shape"] == "string" else raw
    raise ValueError("unknown fixed-write recipe " + rule)


def _assign(value, spec, path, rule, token, default):
    parts = path.replace("[", ".").replace("]", "").split(".")
    owner = value
    current = spec
    for part in parts[:-1]:
        if part.isdigit():
            idx = int(part)
            owner = owner._values[idx] if hasattr(owner, "_values") else owner[idx]
            current = current["children"][0]
        else:
            field = next(field for field in current["children"] if field["name"] == part)
            owner = getattr(owner, part)
            current = field
            if current["shape"] == "optional":
                current = current["children"][0]
    name = parts[-1]
    if name.isdigit():
        idx = int(name)
        elem_spec = current["children"][0]
        slots = owner._values if hasattr(owner, "_values") else owner
        slots[idx] = _convert_fixed(elem_spec, slots[idx], rule, token, default)
        return
    field = next(field for field in current["children"] if field["name"] == name)
    previous = getattr(owner, name)
    if rule == "tag":
        previous.type = int(token)
        return
    setattr(owner, name, _convert_fixed(field, previous, rule, token, default))


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal-fixed"):
        case, unit, root, wire, path, rule, token = row[1:8]
        cls = root_class(unit, root)
        if cls is None or not hasattr(cls, "fixed_save"):
            absent(outdir, case, "fixed root not generated for python")
            continue
        runtime = importlib.import_module(cls.__module__.split(".")[0] + "._schema.fixedform")
        # A committed wire the reader refuses is a reader failure, never a
        # missing feature: let it raise, so the surface is red and not absent.
        value = fresh_or_load(cls, wire)
        special = apply(value, path, rule, token)
        if special is UNREPRESENTABLE:
            write_bytes(outdir, case, b"unrepresentable\n")
            continue
        if not special:
            _assign(value, runtime._ROOTS[root]["spec"], path, rule, token, runtime._default)
        try:
            cls.fixed_save([value])
        except ValueError:
            write_bytes(outdir, case, b"refused\n")
        else:
            write_bytes(outdir, case, b"wrote\n")
