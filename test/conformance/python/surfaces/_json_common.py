"""Strict JSON conformance helpers for the generated table methods."""

from _common import read_bytes, write_bytes


def raise_if_unsupported(error):
    if getattr(error, "reason", "") in ("unsupported", "unsupported_kind") or "unsupported" in getattr(error, "rule", ""):
        raise error


def read_wire(cls, path):
    return cls.load(read_bytes(path))


def write_wire(cls, value):
    return value.save()


def answer(outdir, case, cls, value):
    write_bytes(outdir, case + ".wire", write_wire(cls, value))
    write_bytes(outdir, case, b"read\n" + value.to_json().encode("utf-8"))
