"""Bounded, lossless tokenizer for the hand-editable table text grammar.

Nodes retain byte spans into the original input. Resolution against a table
schema is deliberately separate from parsing: an empty list can represent a
record, a sequence, or a union depending on its destination.
"""
from __future__ import annotations

import json
import re
from dataclasses import dataclass

from . import Refused


class TextError(Refused):
    """A hand-text refusal with a zero-based byte offset."""

    def __init__(self, reason: str, offset: int) -> None:
        super().__init__(reason)
        self.offset = offset


@dataclass(frozen=True)
class Node:
    kind: str
    start: int
    end: int
    value: str
    children: tuple[Node, ...] = ()


_NUMBER = re.compile(rb'-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\Z')
_KEYWORD = re.compile(rb':[A-Za-z_][A-Za-z_0-9./-]*\Z')


class _Parser:
    def __init__(self, data: bytes, max_bytes: int, max_tokens: int,
                 max_nodes: int, max_allocation: int, max_depth: int) -> None:
        self.data = data
        self.pos = 0
        self.tokens = 0
        self.nodes = 0
        self.allocation = 0
        self.max_tokens = max_tokens if max_tokens > 0 else 8192
        self.max_nodes = max_nodes if max_nodes > 0 else 4096
        self.max_allocation = max_allocation if max_allocation > 0 else 65536
        self.max_depth = min(max_depth, 128) if max_depth > 0 else 128
        if len(data) > (max_bytes if max_bytes > 0 else 65536):
            raise TextError("text_limit", 0)
        try:
            data.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise TextError("text_encoding", exc.start) from exc

    def gap(self) -> None:
        d = self.data
        while self.pos < len(d):
            c = d[self.pos]
            if c in (32, 9, 10):
                self.pos += 1
            elif c == 13:
                if self.pos + 1 == len(d) or d[self.pos + 1] != 10:
                    raise TextError("text_syntax", self.pos)
                self.pos += 2
            elif c == 59:
                self.pos += 1
                while self.pos < len(d) and d[self.pos] not in (10, 13):
                    if d[self.pos] == 0:
                        raise TextError("text_syntax", self.pos)
                    self.pos += 1
            else:
                break

    def node(self, depth: int) -> Node:
        self.gap()
        start = self.pos
        d = self.data
        if start >= len(d):
            raise TextError("text_unterminated", start)
        self.tokens += 1
        self.nodes += 1
        if self.tokens > self.max_tokens or self.nodes > self.max_nodes:
            raise TextError("text_limit", start)
        c = d[start]
        if c == 40:
            if depth >= self.max_depth:
                raise TextError("text_limit", start)
            self.pos += 1
            children: list[Node] = []
            while True:
                self.gap()
                if self.pos == len(d):
                    raise TextError("text_unterminated", self.pos)
                if d[self.pos] == 41:
                    self.pos += 1
                    return Node("list", start, self.pos, "", tuple(children))
                children.append(self.node(depth + 1))
        if c == 34:
            self.pos += 1
            escaped = False
            while self.pos < len(d):
                ch = d[self.pos]
                self.pos += 1
                if escaped:
                    escaped = False
                elif ch == 92:
                    escaped = True
                elif ch == 34:
                    raw = d[start:self.pos]
                    try:
                        value = json.loads(raw.decode("utf-8"))
                        value.encode("utf-8")
                    except (ValueError, UnicodeError) as exc:
                        raise TextError("text_encoding", start) from exc
                    self.allocation += len(value.encode("utf-8"))
                    if self.allocation > self.max_allocation:
                        raise TextError("text_limit", start)
                    return Node("string", start, self.pos, value)
            raise TextError("text_unterminated", start)
        if c == 41:
            raise TextError("text_syntax", start)
        while self.pos < len(d) and d[self.pos] not in b' \t\r\n();"':
            self.pos += 1
        atom = d[start:self.pos]
        if atom in (b"true", b"false", b"null"):
            kind = atom.decode("ascii")
        elif _KEYWORD.fullmatch(atom):
            kind = "keyword"
        elif _NUMBER.fullmatch(atom):
            kind = "number"
        else:
            raise TextError("text_syntax", start)
        return Node(kind, start, self.pos, atom.decode("ascii"))


def parse(data: bytes, *, max_bytes: int = 0, max_tokens: int = 0,
          max_nodes: int = 0, max_allocation: int = 0, max_depth: int = 0) -> Node:
    """Parse one text, retaining byte spans; schema resolution is separate."""
    parser = _Parser(data, max_bytes, max_tokens, max_nodes, max_allocation, max_depth)
    parser.gap()
    root = parser.node(0)
    if root.kind != "list":
        raise TextError("text_syntax", root.start)
    parser.gap()
    if parser.pos != len(data):
        raise TextError("text_syntax", parser.pos)
    return root
