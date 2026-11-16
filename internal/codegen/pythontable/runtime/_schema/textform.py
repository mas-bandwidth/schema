"""Strict hand-editable text resolution over generated table descriptors."""
from __future__ import annotations

import base64
import binascii
import decimal
import json
from typing import Any

from . import Refused
from .descriptor import FieldDesc, by_name, new_union_by_name
from .json_table import _array_element, _inner, from_json
from .kind_map import _entry_fields
from .textform_scan import Node, TextError, parse


def _fail(reason: str, node: Node) -> None:
    raise TextError(reason, node.start)


def _name(node: Node) -> str:
    if node.kind == "string":
        return node.value
    if node.kind == "keyword":
        return node.value[1:]
    _fail("text_syntax", node)
    raise AssertionError


def _pairs(node: Node) -> list[tuple[Node, Node]]:
    if node.kind != "list":
        _fail("text_kind_mismatch", node)
    if len(node.children) % 2:
        _fail("text_syntax", node)
    return list(zip(node.children[::2], node.children[1::2]))


def _integer(node: Node, field: FieldDesc) -> str:
    if node.kind != "number":
        _fail("text_kind_mismatch", node)
    try:
        value = decimal.Decimal(node.value)
        if value != value.to_integral_value():
            _fail("text_kind_mismatch", node)
        if value < field.lo or value > field.hi:
            _fail("text_bound", node)
        return str(int(value))
    except decimal.DecimalException:
        _fail("text_bound", node)
    raise AssertionError


def _record(cls: Any, node: Node, pointer: bool, labels: dict[str, str]) -> str:
    pairs = _pairs(node)
    fields = {field.json_key: field for field in cls.descriptor.fields}
    seen: set[str] = set()
    parts: list[str] = []
    label = ""
    if pairs and pairs[0][0].kind == "string" and pairs[0][0].value == "&node":
        key, item = pairs.pop(0)
        if not pointer or item.kind != "number" or not item.value.isascii() or not item.value.isdigit() or item.value.startswith("0") or len(item.value) > 20 or int(item.value) > (1 << 64) - 1:
            _fail("text_reference", key)
        label = item.value
        if not pairs:
            if labels.get(label) != cls.__name__:
                _fail("text_reference", item)
            return '{"&node":' + label + '}'
        if label in labels:
            _fail("text_reference", item)
        labels[label] = ""  # A definition is unavailable until complete.
        parts.append('"&node":' + label)
    raw: dict[str, Node] = {}
    for key, item in pairs:
        name = _name(key)
        if name.startswith("&"):
            _fail("text_reference", key)
        field = fields.get(name)
        if field is None:
            _fail("text_unknown_key", key)
        if name in seen:
            _fail("text_duplicate_key", key)
        seen.add(name)
        raw[field.name] = item
        parts.append(json.dumps(name) + ':' + _value(field, item, labels))
    for field in fields.values():
        guards = getattr(field, "guards", ())
        active = True
        for guard_name, wanted in guards:
            g = next(f for f in fields.values() if f.name == guard_name)
            source = raw.get(guard_name)
            val = g.default if source is None else source.kind == "true"
            if val != wanted:
                active = False
        if field.json_key in seen and not active:
            _fail("text_inactive_field", raw[field.name])
        if field.required and active and field.json_key not in seen:
            _fail("text_missing_required", node)
    if label:
        labels[label] = cls.__name__
    return '{' + ','.join(parts) + '}'


def _value(field: FieldDesc, node: Node, labels: dict[str, str]) -> str:
    codec = field.codec
    if node.kind == "null":
        if codec in ("optional", "pointer", "string_blob", "bytes_blob"):
            return "null"
        _fail("text_kind_mismatch", node)
    if codec == "optional":
        return _value(_inner(field), node, labels)
    if codec in ("table", "pointer"):
        cls = by_name(field.target)
        if cls is None:
            _fail("text_kind_mismatch", node)
        return _record(cls, node, codec == "pointer", labels)
    if codec == "union":
        pairs = _pairs(node)
        if not pairs:
            return '{}'
        if len(pairs) > 1:
            names = [_name(k) for k, _ in pairs]
            _fail("text_duplicate_key" if len(set(names)) != len(names) else "text_kind_mismatch", node)
        key, item = pairs[0]
        name = _name(key)
        union = new_union_by_name(field.target)
        arm = next((a for a in union.descriptor.arms if a.name == name), None)
        if arm is None:
            _fail("text_unknown_key", key)
        if arm.field is None:
            if item.kind != "null":
                _fail("text_kind_mismatch", item)
            value = "null"
        else:
            value = _value(arm.field, item, labels)
        return '{' + json.dumps(name) + ':' + value + '}'
    if field.json_flags and codec not in ("array", "pointer_array", "union_array", "map", "keyed"):
        if node.kind != "list":
            _fail("text_kind_mismatch", node)
        names: list[str] = []
        for item in node.children:
            name = _name(item)
            if name not in field.json_variants:
                _fail("text_unknown_variant", item)
            if name in names:
                _fail("text_duplicate_key", item)
            names.append(name)
        return json.dumps(names)
    if codec == "enum":
        name = _name(node)
        if name != "None" and name not in field.json_variants:
            _fail("text_unknown_variant", node)
        return json.dumps(name)
    if codec == "keyed":
        seen: set[str] = set()
        parts: list[str] = []
        assert field.elem is not None
        for key, item in _pairs(node):
            name = _name(key)
            if name not in field.json_keys:
                _fail("text_unknown_key", key)
            if name in seen:
                _fail("text_duplicate_key", key)
            seen.add(name)
            parts.append(json.dumps(name) + ':' + _value(field.elem, item, labels))
        return '{' + ','.join(parts) + '}'
    if codec == "map":
        if node.kind != "list":
            _fail("text_kind_mismatch", node)
        key_field, val_field = _entry_fields(field)
        seen = set()
        parts = []
        for pair in node.children:
            if pair.kind != "list" or len(pair.children) != 2:
                _fail("text_kind_mismatch", pair)
            key, item = pair.children
            if key_field.codec == "string":
                if key.kind != "string":
                    _fail("text_kind_mismatch", key)
                name = key.value
                if not key_field.unbounded and len(name.encode()) > key_field.size:
                    _fail("text_bound", key)
            else:
                name = _integer(key, key_field)
            if name in seen:
                _fail("text_duplicate_key", key)
            seen.add(name)
            parts.append(json.dumps(name) + ':' + _value(val_field, item, labels))
        return '{' + ','.join(parts) + '}'
    if codec in ("array", "pointer_array", "union_array"):
        if node.kind != "list":
            _fail("text_kind_mismatch", node)
        count = len(node.children)
        if (field.array_kind == 1 and count != field.bound) or (field.array_kind != 3 and count > field.bound):
            _fail("text_bound", node)
        element = _array_element(field)
        if field.json_text_list:
            cls = by_name(element.target)
            if cls is None:
                _fail("text_kind_mismatch", node)
            element = cls.descriptor.fields[0]
        return '[' + ','.join(_value(element, child, labels) for child in node.children) + ']'
    if codec == "bool":
        if node.kind not in ("true", "false"):
            _fail("text_kind_mismatch", node)
        return node.value
    if codec in ("string", "string_blob", "bytes", "bytes_blob") or field.kind == 36:
        if node.kind != "string":
            _fail("text_kind_mismatch", node)
        if codec in ("bytes", "bytes_blob"):
            try:
                decoded = base64.b64decode(node.value, validate=True)
                if base64.b64encode(decoded).decode() != node.value:
                    _fail("text_encoding", node)
            except (ValueError, binascii.Error):
                _fail("text_encoding", node)
            size = len(decoded)
        else:
            size = len(node.value.encode("utf-8"))
        if codec in ("string", "bytes") and not getattr(field, "unbounded", False) and size > field.size:
            _fail("text_bound", node)
        return json.dumps(node.value)
    if codec == "int" and field.kind not in range(20, 30):
        value = _integer(node, field)
        return json.dumps(value) if field.kind in (5, 9, 18, 19) else value
    if node.kind != "number":
        _fail("text_kind_mismatch", node)
    return json.dumps(node.value) if field.kind in (23, 24, 28, 29) else node.value


def load(cls: type[Any], data: bytes) -> tuple[Node, Any]:
    """Resolve one input transactionally through the table JSON walk."""
    root = parse(data)
    text = _record(cls, root, False, {})
    try:
        return root, from_json(cls, text)
    except Refused as exc:
        reasons = {"out_of_range": "text_bound", "length_over_bound": "text_bound", "count_over_bound": "text_bound", "timestamp_not_rfc3339": "text_timestamp", "kind_mismatch": "text_kind_mismatch", "missing_required": "text_missing_required", "malformed": "text_reference"}
        raise TextError(reasons.get(exc.reason, "text_kind_mismatch"), root.start) from exc


class Document:
    """An immutable source document and the value it resolved to."""

    def __init__(self, cls: type[Any], source: bytes, root: Node, value: Any) -> None:
        self._cls = cls
        self.source = source
        self.root = root
        self.value = value

    def write(self) -> bytes:
        """Return the original bytes, including all trivia and line endings."""
        return self.source

    def edit(self, edits: list[tuple[str, bytes]]) -> Document:
        """Replace existing scalar tokens transactionally by original span."""
        from .json_table import to_json
        import re

        replacements: list[tuple[int, int, bytes]] = []
        original = to_json(self.value)
        for path, replacement in edits:
            parts = re.findall(r'([A-Za-z_][A-Za-z_0-9./-]*)|\[([0-9]+)\]', path)
            rebuilt = ''.join((('.' if i and name else '') + name) if name else '[' + index + ']' for i, (name, index) in enumerate(parts))
            if not parts or rebuilt != path:
                raise TextError("text_edit_conflict", 0)
            current = self.root
            for name, index in parts:
                if name:
                    pair = next(((key, val) for key, val in _pairs(current) if _name(key) == name), None)
                    if pair is None:
                        raise TextError("text_edit_conflict", current.start)
                    current = pair[1]
                else:
                    if current.kind != "list" or int(index) >= len(current.children):
                        raise TextError("text_edit_conflict", current.start)
                    current = current.children[int(index)]
            if current.kind == "list":
                raise TextError("text_edit_conflict", current.start)
            probe = parse(b"(" + replacement + b")")
            if len(probe.children) != 1 or probe.children[0].kind == "list":
                raise TextError("text_edit_conflict", current.start)
            candidate = self.source[:current.start] + replacement + self.source[current.end:]
            _, value = load(self._cls, candidate)
            if to_json(value) != original:
                replacements.append((current.start, current.end, replacement))
        replacements.sort()
        for previous, current in zip(replacements, replacements[1:]):
            if previous[1] > current[0] or previous[0] == current[0]:
                raise TextError("text_edit_conflict", current[0])
        result = self.source
        for start, end, replacement in reversed(replacements):
            result = result[:start] + replacement + result[end:]
        return read(self._cls, result)


def read(cls: type[Any], data: bytes) -> Document:
    """Read a document and its typed value, or raise a refusal."""
    root, value = load(cls, data)
    return Document(cls, data, root, value)
