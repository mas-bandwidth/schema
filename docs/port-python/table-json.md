## Table JSON

The table text reader uses the schema JSON scanner and descriptor conversion,
including duplicate-key detection and the schema number rules. The writer
validates values and emits the canonical text with its final newline. Generated
scalar field dispatch uses the same conversion helpers as the descriptor walk;
compound fields use the descriptor walk.

### Bench

On a shared Linux bench, the best of five samples of 10,000 operations on a
five-field scalar table measured the following nanoseconds per operation:

| CPython | Generated read | Descriptor read | Generated write | Descriptor write |
| --- | ---: | ---: | ---: | ---: |
| 3.15.2 | 18,085 | 19,627 | 1,404 | 10,670 |
| 3.11.18 | 23,034 | 23,807 | 1,204 | 9,364 |

The generated and descriptor shapes use the same scanner and value conversion;
the functional benchmark checks their results. These measurements cover the
scalar path, not compound tables.
