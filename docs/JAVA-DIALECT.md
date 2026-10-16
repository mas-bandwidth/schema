# The Java dialect

What generated Java is held to, by which tool, at which release, and how to run
each gate. This is the Java leg's page for docs/PORTING.md J4 (issue #547:
generated code reads as the language's own, and a formatter or analyzer
finding at default strictness is red).

## The formatter

**google-java-format 1.28.0**, the language's one formatting authority, in
its **`--aosp`** style.

- **The pin** is in `make/java.mk` beside the JDK's: the release, its URL
  (`google-java-format-1.28.0-all-deps.jar` from the google/google-java-format
  releases), and its sha256. The sha256 is **owed**: the jar was on neither
  Linux bench on 2026-10-15, so the hash is recorded by whoever installs it,
  from the file itself, and never typed from memory.
- **Where it lives.** Under `dist/` the way the JDK does
  (`dist/google-java-format-1.28.0-all-deps.jar`, gitignored), run on the
  pinned JDK with the six `--add-exports=jdk.compiler/...` flags its README
  names for JDK 16 and later. A launcher elsewhere is passed as
  `GOOGLE_JAVA_FORMAT=<launcher>`. No recipe fetches it and the tree never
  vendors it.
- **The probe.** `GOOGLE_JAVA_FORMAT --version` must answer with the pinned
  release. Anything else is "does not resolve".

### The style, and why `--aosp`

Every Java emitter (`internal/codegen/java`, `internal/codegen/javatable`)
writes four-space indentation. The formatter's default Google style is two
spaces; `--aosp` is the same formatter's four-space style, not a looser
tool. The rule the leg follows is: pick the style the emitters already write
when it is one flag of the same formatter, so the measurement is of drift and
not of a style the emitters never aimed at.

**The decision is the owner's, and the numbers behind it are owed.** The
census (`make tables-java-format-census`) prints, for the table tree and the
packet tree under each style, the files red and the diff lines. It has not
run: no bench carried the formatter on 2026-10-15. What is measured is the
input: 585 generated files, 552 in the 17 table units and 33 in the 8 packet
units. The style is a single variable, `JAVA_FORMAT_STYLE` (default
`--aosp`), so the owner's call is a one-line change.

## The lint

`javac --release 17 -Xlint:all -Werror`, under the pinned JDK 21. Generated
sources are compiled by the consumer's javac, so a warning here is a build
failure in their tree and not only in ours.

## The naming and package rules the emitters follow

- **One public class per file, named for the file.** A schema file
  `Constants.schema` becomes `Constants.java` holding `public final class
  Constants`; its types and unions are nested static classes.
- **One package per unit.** Every file of a unit says `package
  <unitpackage>;`, so cross-file references are plain qualified names and
  generated files never import each other.
- **Members are lowerCamelCase**, the first-letter-lowered form of the
  `ir.GoExportName` mapping the other managed targets share.
- **The table runtime is one file per type** (`TableBytes.java`,
  `TableBlockInfo.java`, ...), so a runtime type's home is its name and never
  a file order. A schema file that spells a runtime type's name is refused by
  name (`checkNames`), never overwritten.
- **Self-contained.** Only `java.lang`, `java.lang.invoke` and `java.util`;
  no runtime package and no JSON library (`tables-java-standalone`).

## The gates, and how to run each

Pass the bench JDK as `JAVA=` and `JAVAC=`; the defaults are a macOS `dist/`
path.

| target | what it holds | runs from |
|---|---|---|
| `tables-java-clean` | google-java-format 1.28.0 `--aosp --dry-run --set-exit-if-changed` over every generated Java file: the 17 table units (`build/tables-generated-java`) and the 8 packet-wire units (`build/java-clean-packet`), counted, refused below 585 | `test-java` |
| `tables-java-compile-all` | every one of those 25 units compiled on its own under `--release 17 -Xlint:all -Werror`, counted, refused below 25 | `tables-java-release` (certify.yml) |
| `tables-java-format-census` | the measurement: files red and diff lines under the default style and under `--aosp` | by hand |
| `tables-java-clean-formatter-missing-negative-control` | with the formatter pointed at nothing, `tables-java-clean` FAILS naming the tool under `SCHEMA_REQUIRE_CORPUS=1`, and SKIPS naming that variable without it | the negative-controls leg, group java |
| `tables-java-clean-drift-negative-control` | the emitter writes one line of `TableBytes.java` a space deeper (`tools/sabotage/table_java_format_drift.go`); `tables-java-clean` must go red naming that file, and the unsabotaged file must be clean first | by hand, until a workflow installs the formatter (make/negative-controls.json, excluded with that reason) |

**The gate does not skip silently.** Without the formatter,
`tables-java-clean` prints `SKIP` and names `SCHEMA_REQUIRE_CORPUS=1` and
`JAVA_REQUIRE_FORMATTER=1`; with either set, it fails and names the tool.

## What is not gated

- **The formatter's verdict on today's emitters.** No run of the real
  formatter has happened. The emitters write one-line method bodies
  (`{ return data[at]; }`) and break long expressions after an operator, both
  of which google-java-format rewrites in either style, so the first real run
  is expected to be red; that is a prediction, not a count.
- **The CI step that installs the formatter.** `.github/workflows/` is outside
  this leg's change; until a workflow installs the pinned jar, CI's
  `tables-java-clean` SKIPs by name, and the drift control runs by hand.
- **Fixtures written by hand** (`test/**/Main.java`, `test/conformance/java`)
  are compiled under the lint by their legs but are not format-checked.
