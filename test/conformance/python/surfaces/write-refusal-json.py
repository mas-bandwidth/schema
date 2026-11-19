"""Test the JSON writer after planting each invalid value."""

from _common import absent, entries, root_class, write_bytes
from _json_common import raise_if_unsupported, read_wire
from importlib import import_module


def run(manifest, outdir):
    setter = import_module("write-refusal")._set
    for row in entries(manifest, "write-refusal-json"):
        case, unit, root, wire, path, rule, token = row[1:8]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        value = read_wire(cls, wire)
        setter(value, path, rule, token)
        try:
            out = value.to_json()
        except ValueError as error:
            raise_if_unsupported(error)
            result = b"refused\n"
        else:
            if rule == "bytes" and b"\x00" in bytes.fromhex(token) and "\\u0000" not in out:
                result = b"wrote without \\u0000\n"
            else:
                result = b"wrote\n"
        write_bytes(outdir, case, result)
