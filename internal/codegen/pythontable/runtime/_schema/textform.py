"""Strict hand-editable text resolution over generated table descriptors."""
from __future__ import annotations

import base64
import binascii
import decimal
import json
from dataclasses import dataclass
from typing import Any, NoReturn

from . import Refused
from .descriptor import FieldDesc, by_name, new_union_by_name
from .json_table import _array_element, _inner, from_json
from .kind_map import _entry_fields
from .textform_scan import Node, TextError, parse, parse_with_comments


def _fail(reason: str, node: Node) -> NoReturn:
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


def _variant(node: Node) -> str:
    if node.kind not in ("string", "keyword"):
        _fail("text_kind_mismatch", node)
    return _name(node)


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
    array_root = cls.descriptor.json_array and not label
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
        resolved = _value(field, item, labels)
        parts.append(resolved if array_root else json.dumps(name) + ':' + resolved)
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
    if array_root:
        return parts[0] if parts else '[]'
    return '{' + ','.join(parts) + '}'


def _value(field: FieldDesc, node: Node, labels: dict[str, str]) -> str:
    codec = field.codec
    if node.kind == "null":
        if codec in ("optional", "text_optional", "pointer", "string_blob", "bytes_blob"):
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
            arm_names = [_name(k) for k, _ in pairs]
            _fail("text_duplicate_key" if len(set(arm_names)) != len(arm_names) else "text_kind_mismatch", node)
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
            name = _variant(item)
            if name not in field.json_variants:
                _fail("text_unknown_variant", item)
            if name in names:
                _fail("text_duplicate_key", item)
            names.append(name)
        return json.dumps(names)
    if codec == "enum":
        name = _variant(node)
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
        if (field.array_kind == 1 and count != field.bound) or (field.array_kind != 3 and count > field.bound) or (field.array_kind == 2 and count < field.array_min):
            _fail("text_bound", node)
        element = _array_element(field)
        if field.json_text_list and element.codec == "table":
            cls = by_name(element.target)
            if cls is None:
                _fail("text_kind_mismatch", node)
            element = cls.descriptor.fields[0]
        total = getattr(field, "text_total", 0)
        if total and element.codec in ("string", "text_optional"):
            if sum(len(child.value.encode("utf-8")) for child in node.children if child.kind == "string") > total:
                _fail("text_bound", node)
        return '[' + ','.join(_value(element, child, labels) for child in node.children) + ']'
    if codec == "bool":
        if node.kind not in ("true", "false"):
            _fail("text_kind_mismatch", node)
        return node.value
    if codec in ("string", "text_optional", "string_blob", "bytes", "bytes_blob") or field.kind == 36:
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
        if codec in ("string", "text_optional", "bytes") and field.size > 0 and not getattr(field, "unbounded", False) and size > field.size:
            _fail("text_bound", node)
        return json.dumps(node.value)
    if codec == "int" and field.kind not in range(20, 30):
        value = _integer(node, field)
        return json.dumps(value) if field.kind in (5, 9, 18, 19) else value
    if node.kind != "number":
        _fail("text_kind_mismatch", node)
    return json.dumps(node.value) if field.kind in (23, 24, 28, 29) else node.value


@dataclass(frozen=True)
class Limits:
    """Resource ceilings; nonpositive values select the standard defaults."""

    bytes: int = 0
    tokens: int = 0
    nodes: int = 0
    allocation: int = 0
    depth: int = 0


@dataclass(frozen=True)
class Comment:
    """A comment's original byte span and the field/element it attaches to."""

    start: int
    end: int
    path: tuple[str | int, ...]


def _comments(source: bytes, root: Node, cls: type[Any], spans: tuple[tuple[int, int], ...]) -> tuple[Comment, ...]:
    result: list[Comment] = []

    def walk(node: Node, owner: tuple[str | int, ...], descriptor: Any,
             pending: list[tuple[int, int]]) -> None:
        # Slots are semantic siblings: one record field or one array element.
        slots: list[tuple[int, int, tuple[str | int, ...], Node, Any]] = []
        if descriptor is not None and hasattr(descriptor, "fields"):
            fields = {f.json_key: f for f in descriptor.fields}
            for key, value in _pairs(node):
                name = _name(key)
                field = fields.get(name)
                if field is not None:
                    slots.append((key.start, value.end, owner + (name,), value, field))
        elif descriptor is not None and getattr(descriptor, "codec", "") in ("array", "pointer_array", "union_array"):
            field = descriptor
            elem = _array_element(field)
            for index, value in enumerate(node.children):
                slots.append((value.start, value.end, owner + (index,), value, elem))
        elif descriptor is not None and getattr(descriptor, "codec", "") == "map":
            _, value_field = _entry_fields(descriptor)
            for pair in node.children:
                key, value = pair.children
                map_key: str | int = key.value
                if key.kind == "number":
                    map_key = int(decimal.Decimal(key.value))
                slots.append((pair.start, pair.end, owner + (map_key,), value, value_field))
        elif descriptor is not None and getattr(descriptor, "codec", "") == "keyed":
            for key, value in _pairs(node):
                slots.append((key.start, value.end, owner + (_name(key),), value, descriptor.elem))
        elif descriptor is not None and getattr(descriptor, "codec", "") == "union":
            union = new_union_by_name(descriptor.target)
            for key, value in _pairs(node):
                arm = next(a for a in union.descriptor.arms if a.name == _name(key))
                slots.append((key.start, value.end, owner + (arm.name,), value, arm.field))
        elif descriptor is not None and getattr(descriptor, "json_flags", False):
            for index, value in enumerate(node.children):
                slots.append((value.start, value.end, owner + (index,), value, None))

        nested: dict[int, list[tuple[int, int]]] = {}
        for start, end in pending:
            containing = next((i for i, slot in enumerate(slots) if slot[3].kind == "list" and slot[3].start < start < slot[3].end), None)
            if containing is not None:
                nested.setdefault(containing, []).append((start, end))
                continue
            inside = next((slot for slot in slots if slot[0] < start < slot[1]), None)
            if inside is not None:
                result.append(Comment(start, end, inside[2]))
                continue
            prev = next((slot for slot in reversed(slots) if slot[1] <= start), None)
            after = next((slot for slot in slots if slot[0] > start), None)
            if prev is not None and b"\n" not in source[prev[1]:start]:
                path = prev[2]
            else:
                path = after[2] if after is not None else owner
            result.append(Comment(start, end, path))
        for index, items in nested.items():
            _, _, path, value, field = slots[index]
            while getattr(field, "codec", "") == "optional":
                field = _inner(field)
            codec = getattr(field, "codec", "")
            if codec in ("table", "pointer"):
                target = by_name(field.target)
                child_desc = target.descriptor if target is not None else None
            else:
                child_desc = field
            walk(value, path, child_desc, items)

    inside = []
    for start, end in spans:
        if root.start < start < root.end:
            inside.append((start, end))
        else:
            result.append(Comment(start, end, ()))
    walk(root, (), cls.descriptor, inside)
    return tuple(sorted(result, key=lambda comment: comment.start))


def load(cls: type[Any], data: bytes, limits: Limits = Limits()) -> tuple[Node, Any]:
    """Resolve one input transactionally through the table JSON walk."""
    root = parse(data, max_bytes=limits.bytes, max_tokens=limits.tokens,
                 max_nodes=limits.nodes, max_allocation=limits.allocation, max_depth=limits.depth)
    return root, _land(cls, root)


def _land(cls: type[Any], root: Node) -> Any:
    text = _record(cls, root, False, {})
    try:
        return from_json(cls, text)
    except Refused as exc:
        reasons = {"out_of_range": "text_bound", "length_over_bound": "text_bound", "count_over_bound": "text_bound", "timestamp_not_rfc3339": "text_timestamp", "kind_mismatch": "text_kind_mismatch", "missing_required": "text_missing_required", "malformed": "text_reference"}
        raise TextError(reasons.get(exc.reason, "text_kind_mismatch"), root.start) from exc


class Document:
    """An immutable source document and the value it resolved to."""

    def __init__(self, cls: type[Any], source: bytes, root: Node, value: Any,
                 limits: Limits, spans: tuple[tuple[int, int], ...]) -> None:
        self._cls = cls
        self.source = source
        self.root = root
        self.value = value
        self.limits = limits
        self.comments = _comments(source, root, cls, spans)

    def write(self) -> bytes:
        """Return the original bytes, including all trivia and line endings."""
        return self.source

    def edit(self, edits: list[tuple[str | tuple[str | int, ...], bytes]]) -> Document:
        """Replace existing scalar tokens transactionally by original span."""
        from .json_table import to_json
        import re

        replacements: list[tuple[int, int, bytes]] = []
        original = to_json(self.value)
        for path, replacement in edits:
            if isinstance(path, str):
                parts = re.findall(r'([A-Za-z_][A-Za-z_0-9/-]*)|\[([0-9]+)\]', path)
                rebuilt = ''.join((('.' if i and name else '') + name) if name else '[' + index + ']' for i, (name, index) in enumerate(parts))
                if not parts or rebuilt != path:
                    raise TextError("text_edit_conflict", 0)
            else:
                if not path or any(not isinstance(p, (str, int)) or isinstance(p, bool) or p == "" or (isinstance(p, int) and p < 0) for p in path):
                    raise TextError("text_edit_conflict", 0)
                parts = [(p, "") if isinstance(p, str) else ("", str(p)) for p in path]
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
            if (len(probe.children) != 1 or probe.children[0].kind == "list"
                    or probe.children[0].start != 1 or probe.children[0].end != len(replacement) + 1):
                raise TextError("text_edit_conflict", current.start)
            candidate = self.source[:current.start] + replacement + self.source[current.end:]
            _, value = load(self._cls, candidate, self.limits)
            if to_json(value) != original:
                replacements.append((current.start, current.end, replacement))
        replacements.sort()
        for previous, following in zip(replacements, replacements[1:]):
            if previous[1] > following[0] or previous[0] == following[0]:
                raise TextError("text_edit_conflict", following[0])
        result = self.source
        for start, end, replacement in reversed(replacements):
            result = result[:start] + replacement + result[end:]
        return read(self._cls, result, self.limits)


def read(cls: type[Any], data: bytes, limits: Limits = Limits()) -> Document:
    """Read a document and its typed value, or raise a refusal."""
    data = bytes(data)
    root, spans = parse_with_comments(data, max_bytes=limits.bytes, max_tokens=limits.tokens,
                                      max_nodes=limits.nodes, max_allocation=limits.allocation,
                                      max_depth=limits.depth)
    return Document(cls, data, root, _land(cls, root), limits, spans)
