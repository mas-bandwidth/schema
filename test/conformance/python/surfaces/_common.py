"""Shared helpers for the Python conformance surfaces."""

import importlib
import os
import sys

ROOT = os.environ.get("PYTHON_CONFORMANCE_GENERATED") or "build/conformance-python"


def entries(manifest, kind):
    out = []
    with open(manifest, "r", encoding="utf-8") as f:
        for raw in f:
            text = raw.strip()
            if not text or text.startswith("#"):
                continue
            fields = text.split()
            if fields[0] == kind:
                out.append(fields)
    return out


def read_bytes(path):
    with open(path, "rb") as f:
        return f.read()


def write_bytes(outdir, name, data):
    with open(os.path.join(outdir, name), "wb") as f:
        f.write(data)


def absent(outdir, name, reason):
    write_bytes(outdir, name + ".absent", str(reason).encode("utf-8"))


def unit_package(key):
    unitdir = os.path.join(ROOT, key)
    if not os.path.isdir(unitdir):
        return None
    for name in sorted(os.listdir(unitdir)):
        if name.startswith("_") or name == "__pycache__":
            continue
        if os.path.isfile(os.path.join(unitdir, name, "__init__.py")):
            if unitdir not in sys.path:
                sys.path.insert(0, unitdir)
            package = name
            return importlib.import_module(package)
    return None


def root_class(unit, root):
    package = unit_package(unit)
    if package is None:
        return None
    return getattr(package, root, None)


def supported(cls):
    seen = set()

    def walk(descriptor):
        if descriptor.name in seen:
            return True
        seen.add(descriptor.name)
        for field in descriptor.fields:
            if field.codec == "unsupported":
                return False
            if field.codec in ("table", "pointer") and field.target:
                from importlib import import_module

                target = None
                for module_name, module in list(sys.modules.items()):
                    if module is None:
                        continue
                    candidate = getattr(module, field.target, None)
                    if candidate is not None and getattr(candidate, "descriptor", None) is not None:
                        target = candidate
                        break
                if target is None:
                    return False
                if not walk(target.descriptor):
                    return False
        return True

    return walk(cls.descriptor)
