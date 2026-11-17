"""Shared cook-opening helpers for the Python conformance surfaces."""

from importlib import import_module


def open_cook(package, root, data, base_offset=0):
    cook = import_module(package.__name__ + "._schema.cook")
    cls = getattr(package, root)
    try:
        return cook.open_cook(cls, data, base_offset)
    except package.Refused:
        return None
