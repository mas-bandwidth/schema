"""Exercise whole-batch write refusal after changing a loaded value."""

from _common import absent, entries, read_bytes, root_class, unit_package, write_bytes


def _target(value, path):
    node = value
    for part in path.replace("[", ".").replace("]", "").split("."):
        node = node[int(part)] if part.isdigit() else getattr(node, part)
    return node


def run(manifest, outdir):
    connections = {row[1]: row for row in entries(manifest, "connection")}
    for row in entries(manifest, "write-refusal-message"):
        case, connection, root, wire, path, kind, token = row[1:8]
        unit = connections[connection][2]
        package = unit_package(unit)
        cls = root_class(unit, root)
        message = getattr(getattr(package, "_schema", None), "_message_unit", None)
        if package is None or cls is None or message is None:
            absent(outdir, case, "message form is not emitted for this root")
            continue
        value = cls.load(read_bytes(wire))
        parts = path.replace("[", ".").replace("]", "").split(".")
        owner = _target(value, ".".join(parts[:-1])) if len(parts) > 1 else value
        current = owner[int(parts[-1])] if parts[-1].isdigit() else getattr(owner, parts[-1])
        if kind in ("int", "ordinal"):
            converted = int(token)
        elif kind == "length":
            converted = "x" * int(token) if isinstance(current, str) else bytes(int(token))
        elif kind == "float":
            converted = float(token)
        elif kind == "bytes":
            converted = bytes.fromhex(token)
            if isinstance(current, str):
                try:
                    converted = converted.decode("utf-8")
                except UnicodeDecodeError:
                    pass
        elif kind == "bool":
            converted = token.lower() in ("true", "1")
        else:
            converted = token
        if parts[-1].isdigit():
            owner[int(parts[-1])] = converted
        else:
            setattr(owner, parts[-1], converted)
        refused = []
        for verb in (message.measure_messages, message.save_messages):
            try:
                verb(cls, [value])
            except package.InvalidValue:
                refused.append(True)
            else:
                refused.append(False)
        if refused[0] != refused[1]:
            raise AssertionError("message measure and save disagree")
        write_bytes(outdir, case, b"refused\n" if refused[0] else b"wrote\n")
