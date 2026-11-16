"""The generated Python table codecs on the conformance verdict pipe."""

import importlib.util
import os
import struct
import sys

GENERATED = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "build/conformance-python")
stdin = sys.stdin.buffer
stdout = sys.stdout.buffer
units = {}


def exact(size):
    data = stdin.read(size)
    if len(data) != size:
        raise EOFError("truncated wire-fuzz frame")
    return data


def name():
    size, = struct.unpack("<H", exact(2))
    return exact(size).decode("utf-8")


def unit(key):
    if key in units:
        return units[key]
    if not key or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-" for c in key):
        raise ValueError("invalid unit key")
    directory = os.path.join(GENERATED, key)
    if not os.path.isdir(directory):
        units[key] = None
        return None
    packages = [n for n in sorted(os.listdir(directory)) if os.path.isfile(os.path.join(directory, n, "__init__.py"))]
    if len(packages) != 1:
        raise RuntimeError("expected one generated package for " + key)
    path = os.path.join(directory, packages[0], "__init__.py")
    alias = "_wirefuzz_" + key.encode("utf-8").hex()
    spec = importlib.util.spec_from_file_location(alias, path, submodule_search_locations=[os.path.dirname(path)])
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot import generated package " + key)
    module = importlib.util.module_from_spec(spec)
    sys.modules[alias] = module
    spec.loader.exec_module(module)
    units[key] = module
    return module


def main():
    header = stdin.read(4)
    if not header:
        return
    if len(header) != 4:
        raise EOFError("truncated roster count")
    count, = struct.unpack("<I", header)
    roster = []
    for _ in range(count):
        key, root = name(), name()
        form, retain = exact(2)
        module = unit(key)
        cls = getattr(module, root, None) if module else None
        if cls is not None and getattr(cls, "descriptor", None) is None:
            cls = None
        if form == 2 and (module is None or not hasattr(module, "load_messages") or not hasattr(cls, "fixed_load")):
            cls = None
        if form not in (1, 2) or retain:
            cls = None
        roster.append((module, cls, form))
    stdout.write(bytes(int(cls is not None) for _, cls, _ in roster))
    stdout.flush()
    while True:
        header = stdin.read(8)
        if not header:
            return
        if len(header) != 8:
            raise EOFError("truncated mutant header")
        index, size = struct.unpack("<II", header)
        data = exact(size)
        module, cls, form = roster[index]
        if cls is None:
            raise RuntimeError("mutant for absent roster entry")
        report = module.LoadReport()
        loaded = refused = False
        saved = None
        try:
            if form == 2:
                vocab = module.MessageVocabulary()
                module.announce_read(vocab, module.ANNOUNCEMENT)
                value = module.load_messages(cls, vocab, data, report=report)
            elif hasattr(cls, "fixed_load"):
                value = cls.fixed_load(data, capacity=len(data) // 9, report=report)
            else:
                value = cls.load(data, report=report)
            loaded = True
        except module.Refused:
            refused = True
        if loaded:
            try:
                if form == 2:
                    saved = module.save_messages(cls, value)
                elif hasattr(cls, "fixed_load"):
                    saved = cls.fixed_save(value)
                else:
                    saved = value.save()
            except module.InvalidValue:
                pass
        stdout.write(struct.pack("<BiiiiiBBqiiq", int(loaded), report.unknown, 0, report.widened, 0, 0,
                                 0, int(refused), -1, report.retained, report.retain_lost,
                                 len(saved) if saved is not None else -1))
        if saved is not None:
            stdout.write(saved)
        stdout.flush()


if __name__ == "__main__":
    main()
