"""The Python conformance driver (test/conformance/README.md).

The driver script execs this file once per surface. A surface is a Python
module in surfaces/ named for the surface; list prints the ones present. The
generated tree is PYTHON_CONFORMANCE_GENERATED, default build/conformance-python,
so a negative control can point the same driver at a sabotaged copy.
"""

import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SURFACES = os.path.join(HERE, "surfaces")


def surface_names():
    if not os.path.isdir(SURFACES):
        return []
    names = []
    for name in os.listdir(SURFACES):
        if name.endswith(".py") and not name.startswith("_"):
            names.append(name[:-3])
    return sorted(names)


def load_surface(name):
    path = os.path.join(SURFACES, name + ".py")
    spec = importlib.util.spec_from_file_location("schema_python_surface_" + name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load the surface " + name)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main(argv):
    if len(argv) == 3 and argv[2] == "list":
        for name in surface_names():
            print(name)
        return 0
    if len(argv) == 4:
        name = argv[2]
        path = os.path.join(SURFACES, name + ".py")
        if not os.path.isfile(path):
            return 2
        os.makedirs(argv[3], exist_ok=True)
        sys.path.insert(0, HERE)
        sys.path.insert(0, SURFACES)
        module = load_surface(name)
        module.run(argv[1], argv[3])
        return 0
    sys.stderr.write("usage: main.py <manifest> list\n       main.py <manifest> <surface> <outdir>\n")
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
