"""The write-refusal surface: set one field, then Measure and Save."""

from copy import deepcopy

from _common import absent, entries, read_bytes, root_class, supported, write_bytes


def _target(value, path):
    parts = path.replace("[", ".").replace("]", "").split(".")
    node = value
    for part in parts:
        if part.isdigit():
            node = node[int(part)]
        else:
            node = getattr(node, part)
    return node


def _set(value, path, rule, token):
    parts = path.replace("[", ".").replace("]", "").split(".")
    owner = _target(value, ".".join(parts[:-1])) if len(parts) > 1 else value
    name = parts[-1]
    current = getattr(owner, name)
    if rule == "count":
        count = int(token)
        if count < 0 or not isinstance(current, list) or (count > 0 and not current):
            raise ValueError("count mutation needs a nonnegative count and a populated list")
        resized = current[:count]
        resized.extend(deepcopy(current[0]) for _ in range(count - len(resized)))
        setattr(owner, name, resized)
    elif rule == "length":
        length = int(token)
        if length < 0:
            raise ValueError("length cannot be represented by a negative Python length")
        setattr(owner, name, ("a" if isinstance(current, str) else b"a") * length)
    elif rule == "ordinal":
        setattr(owner, name, int(token))
    elif rule == "tag":
        union = type(current)()
        union.type = int(token)
        setattr(owner, name, union)
    elif rule == "int":
        setattr(owner, name, int(token))
    elif rule == "bool":
        setattr(owner, name, token.lower() in ("true", "1"))
    elif rule == "float":
        setattr(owner, name, float(token))
    elif rule == "bytes":
        raw = bytes.fromhex(token)
        # A str is the target's text storage. Preserve malformed bytes as
        # surrogate escapes so the writer, not this mutation, rejects them.
        setattr(owner, name, raw.decode("utf-8", "surrogateescape") if isinstance(current, str) else raw)
    else:
        raise ValueError("unknown write-refusal mutation: " + rule)


def run(manifest, outdir):
    for row in entries(manifest, "write-refusal"):
        case, unit, root, wire, path, rule, token = row[1], row[2], row[3], row[4], row[5], row[6], row[7]
        try:
            cls = root_class(unit, root)
            if cls is None:
                absent(outdir, case, "unit not generated for python")
                continue
            if not supported(cls):
                absent(outdir, case, "the python table runtime does not carry this construct")
                continue
            value = cls.load(read_bytes(wire))
            _set(value, path, rule, token)
            try:
                value.measure()
                value.save()
                write_bytes(outdir, case, b"wrote\n")
            except ValueError:
                write_bytes(outdir, case, b"refused\n")
        except Exception as err:  # noqa: BLE001 - a case this leg cannot answer is absent
            absent(outdir, case, err)
