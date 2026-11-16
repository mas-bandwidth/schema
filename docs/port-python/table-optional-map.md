# Optional maps

A Python optional map is `None` when absent and a dictionary when present.
Presence is preserved for an empty dictionary. A present map uses the same
ordered entry framing as a plain map, with the same key and value validation.
Its table and pointer values participate in the ordinary graph validation and
node numbering walk.

The optional-map probes compare absent, empty, populated, and pointer-valued
maps with Go and generated C++ form-1 wire bytes. They exercise both pinned
Python interpreters and strict mypy, validation failures, and planted presence
and graph-numbering defects.
