# Populated implementation witnesses

These are ordinary positive instances in the main conformance manifest. Every
language driver must read and rewrite the same wire and JSON pins; declarations
or empty defaults cannot satisfy the full-implementation prerequisite.

`Root` exercises required content, timestamp, one-sided range, present optional
map, optional variable closure containing a pointer, bounded and optional text
lists, a total text budget, unbounded list text and map keys, declared forms,
and CRC32C integrity. `ArrayRoot` exercises populated array-root JSON.

The `.input.json` files are authoring inputs. The binary files and canonical
`tables/json/feature_*.json` files were produced by compiled generated Go using
the standalone runtime, independently of the neutral codec used by the gate.
`TestPopulatedFeatureWitnessNativePins` compares both representations without
rewriting them. Its explicit `-update-feature-witnesses` flag is for reviewed
fixture maintenance, never part of a normal gate or nightly run.

`TestPopulatedFeatureWitnessesCoverEveryRequiredConstruct` checks all eleven
constructs and proves removing either fixture fails. The gate decodes each
canonical input once before iterating languages; the nightly comparison remains
linear in the language count and does not reread every writer through a reference.
