"""Typed declaration rows for a unit view."""

from __future__ import annotations

from typing import NamedTuple

from .descriptor import FieldDesc, TableDesc


class ViewType(NamedTuple):
    name: str
    file: str
    table: bool
    type: TableDesc
    doc: str
    tags: tuple[str, ...]

class ViewVariant(NamedTuple):
    value: int
    name: str
    id: int
    payload_name: str | None
    payload: TableDesc | None
    field: FieldDesc | None
    doc: str
    tags: tuple[str, ...]

class ViewVocabulary(NamedTuple):
    name: str
    file: str
    max: int
    storage_bits: int
    payload_offset: int
    variants: tuple[ViewVariant, ...]
    doc: str
    tags: tuple[str, ...]

class ViewConstant(NamedTuple):
    name: str
    file: str
    type_name: str
    is_float: bool
    int_value: int
    float_value: float
    doc: str
    tags: tuple[str, ...]

class UnitViewInfo(NamedTuple):
    package: str
    protocol_id: int
    types: tuple[ViewType, ...]
    tables: tuple[ViewType, ...]
    constants: tuple[ViewConstant, ...]
    enums: tuple[ViewVocabulary, ...]
    flags: tuple[ViewVocabulary, ...]
    unions: tuple[ViewVocabulary, ...]
