# Python cards

Generate the `Card` unit with `make cards-python-generate`, then put
`build/cards-python` and `tables/cards/python` on the Python import path.
The generated package and its runtime are used by the facade:

```python
from facade import load, render, set_holder, task_hash

with open("card.wire", "rb") as source:
    record = load(source.read())
print(render(record))
print(task_hash(record))
set_holder(record, "worker")
with open("card-edited.wire", "wb") as destination:
    destination.write(record.wire)
```

`read_strict` refuses a card containing unrecognized data. `parse` builds a
record from a brief; `from_json` and `to_json` use the
strict generated JSON mapping. `upgrade` retains fields the current declaration
cannot name, and the `set_*` edits refuse a lossy record rather than discard
unknown data. A refusal raises `CardError` with its reason class in `reason`.
An unsuccessful edit leaves the record's wire unchanged.

`make cards-python-conformance` runs the shared nine-surface corpus under both
pinned Python interpreters. `make cards-python-test` also checks the facade,
C++ and Go wire and JSON identity, and strict mypy on the generated package.
