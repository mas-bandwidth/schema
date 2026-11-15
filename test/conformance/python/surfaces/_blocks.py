"""Shared block helpers for the Python conformance surfaces."""

BLOCK_ROOTS = {"block_render": "RenderFrame", "block_padded": "PaddedFrame"}


def block_root(name):
    for prefix, root in BLOCK_ROOTS.items():
        if name.startswith(prefix):
            return root
    return None


def open_block(package, root, data, base_offset=0):
    from importlib import import_module

    block = import_module(package.__name__ + "._schema.block")
    try:
        return block.BlockOpen(package, root, data, package.BUILD_VERSION, base_offset)
    except Exception:
        return None


def open_raw(package, root, data):
    from importlib import import_module

    block = import_module(package.__name__ + "._schema.block")
    try:
        handle = block.BlockOpen(package, root, data, package.BUILD_VERSION)
    except Exception as err:  # noqa: BLE001 - a case this leg cannot open says why
        return None, str(err)
    return handle, lambda: block.Dump(handle)


def foreign(source):
    if len(source) < 8:
        return source
    return source[0:8][::-1] + source[8:]


def place(source, extent):
    if extent < 0 or extent == len(source):
        return source
    if extent < len(source):
        return source[:extent]
    return source + b"\0" * (extent - len(source))
