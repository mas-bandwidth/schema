"""Shared helpers for the Python conformance surfaces."""

import contextlib
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
    """A `.absent` naming a reason is the leg's REFUSAL as its answer; on a row
    that owes a refusal the harness scores it as the refusal. A refusal is
    never spelled empty: that is the no-codec absence below."""
    text = str(reason).strip() or "refused"
    write_bytes(outdir, name + ".absent", text.encode("utf-8"))


def no_codec(outdir, name):
    """A case this leg has NO CODEC for (its unit or root is not generated, or
    the form is not emitted) is an EMPTY `.absent`: the per-case absence of
    test/conformance/README.md, which counts absent and never as a refusal, so
    a missing codec cannot pass a must-refuse row."""
    write_bytes(outdir, name + ".absent", b"")


# The cases that hit a driver error in this surface (see answering below), and
# the surface main.py is running, for the driver-error line.
DRIVER_ERRORS = []
SURFACE = ""


@contextlib.contextmanager
def answering(name):
    """Answer ONE case. A case has three outcomes and the driver tells them
    apart by mechanism, never by name: no codec for it (no_codec), the present
    codec's answer (bytes, or its refusal), or a DRIVER ERROR. A driver error
    names its case on stderr and the cases behind it still run; the surface
    then exits non-zero, so the harness reds it (README.md: exit codes)."""
    try:
        yield
    except Exception as err:  # every driver error, whatever its class
        DRIVER_ERRORS.append(name)
        sys.stderr.write(f"{SURFACE} {name}: driver error: {type(err).__name__}: {err}\n")


def unit_package(key, tree=None):
    unitdir = os.path.join(ROOT if tree is None else tree, key)
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


def root_class(unit, root, tree=None):
    package = unit_package(unit, tree)
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
            if not carried(field):
                return False
            # An array's element (and a keyed array's slot) is a field of its
            # own: an unsupported element or a child table behind it decides
            # too.
            for name in ("element", "elem"):
                elem = getattr(field, name, None)
                if elem is not None and not carried(elem):
                    return False
        return True

    def carried(field):
        if field.codec == "unsupported":
            return False
        if field.codec in ("table", "pointer", "pointer_array") and field.target:
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
