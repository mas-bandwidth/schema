"""Exercise whole-batch write refusal after changing a loaded value."""

from _common import answering, no_codec, entries, read_bytes, root_class, unit_package, write_bytes


def _target(value, path):
    node = value
    for part in path.replace("[", ".").replace("]", "").split("."):
        if part.isdigit():
            idx = int(part)
            node = node._values[idx] if hasattr(node, "_values") else node[idx]
        else:
            node = getattr(node, part)
    return node


def run(manifest, outdir):
    connections = {row[1]: row for row in entries(manifest, "connection")}
    for row in entries(manifest, "write-refusal-message"):
        with answering(row[1]):
            case, connection, root, wire, path, kind, token = row[1:8]
            unit = connections[connection][2]
            package = unit_package(unit)
            cls = root_class(unit, root)
            message = getattr(getattr(package, "_schema", None), "_message_unit", None)
            if package is None or cls is None or message is None:
                no_codec(outdir, case)
                continue
            value = cls.load(read_bytes(wire))
            parts = path.replace("[", ".").replace("]", "").split(".")
            owner = _target(value, ".".join(parts[:-1])) if len(parts) > 1 else value
            last = parts[-1]
            if last.isdigit():
                slots = owner._values if hasattr(owner, "_values") else owner
                current = slots[int(last)]
            else:
                current = getattr(owner, last)
            if kind in ("int", "ordinal"):
                converted = int(token)
            elif kind == "tag":
                u = type(current)()
                u.type = int(token)
                converted = u
            elif kind == "length":
                converted = "x" * int(token) if isinstance(current, str) else bytes(int(token))
            elif kind == "float":
                converted = float(token)
            elif kind == "bytes":
                raw = bytes.fromhex(token)
                converted = raw.decode("utf-8", "surrogateescape") if isinstance(current, str) else raw
            elif kind == "bool":
                converted = token.lower() in ("true", "1")
            else:
                converted = token
            if last.isdigit():
                slots = owner._values if hasattr(owner, "_values") else owner
                slots[int(last)] = converted
            else:
                setattr(owner, last, converted)
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
