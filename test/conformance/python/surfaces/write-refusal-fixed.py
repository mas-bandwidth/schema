"""The fixed writer's declaration contract, over every manifest recipe."""

import importlib

from _common import absent, entries, read_bytes, root_class, write_bytes


def _assign(value, spec, path, rule, token, default):
    parts = path.replace("[", ".").replace("]", "").split(".")
    owner = value
    current = spec
    for part in parts[:-1]:
        if part.isdigit():
            owner = owner[int(part)]
            current = current["children"][0]
        else:
            field = next(field for field in current["children"] if field["name"] == part)
            owner = getattr(owner, part)
            current = field
            if current["shape"] == "optional":
                current = current["children"][0]
    name = parts[-1]
    field = next(field for field in current["children"] if field["name"] == name)
    previous = getattr(owner, name)
    if rule in ("int", "ordinal"):
        value = int(token)
    elif rule == "float":
        value = float(token)
    elif rule == "tag":
        previous.type = int(token)
        return
    elif rule == "count":
        count = int(token)
        value = list(previous[:count])
        while len(value) < count:
            value.append(default(field["children"][0]))
    elif rule == "length":
        count = int(token)
        value = "x" * count if field["shape"] == "string" else bytes(count)
    elif rule == "bytes":
        raw = bytes.fromhex(token)
        # A Python text value can express invalid UTF-8 only as an invalid
        # scalar. Surrogate escape keeps that witness for the writer's check.
        value = raw.decode("utf-8", "surrogateescape") if field["shape"] == "string" else raw
    else:
        raise ValueError("unknown fixed-write recipe " + rule)
    setattr(owner, name, value)


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal-fixed"):
        case, unit, root, wire, path, rule, token = row[1:8]
        cls = root_class(unit, root)
        if cls is None or not hasattr(cls, "fixed_save"):
            absent(outdir, case, "fixed root not generated for python")
            continue
        runtime = importlib.import_module(cls.__module__.split(".")[0] + "._schema.fixedform")
        try:
            value = cls.load(read_bytes(wire))
        except ValueError as err:
            absent(outdir, case, err)
            continue
        try:
            _assign(value, runtime._ROOTS[root]["spec"], path, rule, token, runtime._default)
            cls.fixed_save([value])
        except ValueError:
            write_bytes(outdir, case, b"refused\n")
        else:
            write_bytes(outdir, case, b"wrote\n")
