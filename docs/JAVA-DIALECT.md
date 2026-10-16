# The Java dialect

What generated Java is held to, by which tool, at which release, and how to run
each gate. This is the Java leg's page for docs/PORTING.md J4 (issue #547:
generated code reads as the language's own, and a formatter or analyzer
finding at default strictness is red).

## The formatter

**google-java-format 1.37.0**, the language's one formatting authority, in
its **`--aosp`** style.

- **The pin** is in `make/java.mk` beside the JDK's: the release, its URL
  (`google-java-format-1.37.0-all-deps.jar` from the google/google-java-format
  releases), and its sha256,
  `834b2a0c38cb774953322a84b5ca3f2f40dd3156650b3cd44d3b744345962f7a`, read
  with `sha256sum` off the jar the fleet's toolchains role installed on both
  Linux benches on 2026-10-15 (the two agree).
- **Where it lives.** Either under `dist/` the way the JDK does
  (`dist/google-java-format-1.37.0-all-deps.jar`, gitignored), run on the
  pinned JDK with the six `--add-exports=jdk.compiler/...` flags its README
  names for JDK 16 and later; or, when that jar is absent, the
  `google-java-format` launcher on PATH (the benches carry it in `~/sdk/bin`).
  `GOOGLE_JAVA_FORMAT=<launcher>` overrides both. No recipe fetches it and the
  tree never vendors it.
- **The probe.** `GOOGLE_JAVA_FORMAT --version` must answer the pinned
  release's own line, whole (`google-java-format: Version 1.37.0`). Anything
  else is "does not resolve", including `java -jar` over a missing jar whose
  path happens to carry the version.

### The style, and why `--aosp`

Every Java emitter (`internal/codegen/java`, `internal/codegen/javatable`)
writes four-space indentation. The formatter's default Google style is two
spaces; `--aosp` is the same formatter's four-space style, not a looser
tool. The rule the leg follows: pick the style the emitters already write
when it is one flag of the same formatter, so the gate measures drift and
not a style the emitters never aimed at.

**The measurement** (`make tables-java-format-census`, google-java-format
1.37.0, 585 generated files: 552 in the 17 table units, 33 in the 8 packet
units):

| | default Google style | `--aosp` |
|---|---|---|
| at the base, before any emitter fix | 585 of 585 files red, 345,331 diff lines | 528 of 585 files red, 124,743 diff lines |
| after the emitter fixes of #547 | 585 of 585 files red, 403,353 diff lines | **0 of 585 files red** |

**The decision**, stated for the owner to overrule: `--aosp`. It is the
style the emitters write, and the only one of the two that differs from the
emitted indentation by drift rather than by design. The style is a single
variable, `JAVA_FORMAT_STYLE` (default `--aosp`), so the other call is a
one-line change plus the emitter work the Google column counts.

## How the emitters write the formatter's shape

Most of each emitter is fixed template text, and the drift it had was in two
places: the parts whose width depends on a name (comments filled to the
column, calls that overflow it), and a handful of whitespace rules every
template broke the same way. The fixes are of three kinds, each in the
emitter and never in generated output:

- **Fixed runtime text is written in the formatter's shape.** The constant
  runtime sources (`TableFixed`, `TableVariable`, `TableJson`, `TableRetain`,
  `TableVocabulary`, `TableBytes`, `TableBlockRows`, `TableBlockFieldInfo`,
  `TableCookStorage`, `TableCookFieldInfo`) are the formatter's own output for
  them, held as the template.
- **Templates that indented or laid out wrongly were fixed where they were
  wrong**: the descriptor `build()` bodies (block.go, rows.go, view.go), the
  union arms of the fixed form (fixedform.go), the layout literal's commented
  rows (fixedform.go, `fixedFillRow`), the enum switch of the variable form
  (variableform.go, retain.go), the layout-check message (block.go, cook.go).
- **One last pass, `java.Canonical`** (internal/codegen/java/java.go), applied
  to every file both Java emitters write: Javadoc of plain prose refilled to
  the 100-column limit (one line when it fits, `<p>` between paragraphs); a
  line comment past the limit refilled at its words; a one-line block opened
  onto its own lines; one statement a line; `case` labels a line each;
  `else` joined to its brace; array initializers and argument lists laid out
  the formatter's way (items shorter than ten columns filled, otherwise all on
  one line or one a line); a statement past the limit broken after its `=`,
  inside its call, before its loosest operator, or before each call of a
  chain (a call's arguments, a conditional's arms, a binary chain's operands
  and a cast's operand broken in turn when they still do not fit); unused
  imports dropped; the formatter's blank lines between members and after the
  imports.
  **Compact templates are laid out once, in the same pass** (`relayout`):
  the emitters added with maps, unions, keyed arrays, wide integers and the
  cook writer write statement groups on one line, unspaced
  (`if(kind==15){v.x=...;}else{...}`), sometimes at column 0. A line whose
  token spacing is not the standard spacing, or that holds a statement after
  a statement or a block's opening and its contents, is re-laid one
  statement a line, with the standard spacing, and the lines of the blocks
  it opens are re-laid with it at the block's indentation (a line written at
  column 0 takes its indentation from the code above it). A line already in
  the formatter's shape is never re-laid, so **it never moves the indentation
  of such a line**: a template that indents a well-formed line wrongly is a
  template the gate sees, which is exactly what the drift control plants.
  `TestRelayoutLaysOutCompactTemplatesOnly` pins both halves.

`TestCanonicalMovesLayoutNotTokens` (in both packages) holds the pass to that:
over every corpus the gate formats, the code tokens and the words of every
comment are unchanged (an unused import aside), and a second pass changes
nothing.

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
- **Self-contained.** Only `java.lang`, `java.lang.invoke`, `java.nio` and
  `java.util`; no runtime package and no JSON library
  (`tables-java-standalone`).

## The gates, and how to run each

Pass the bench JDK as `JAVA=` and `JAVAC=`; the defaults are a macOS `dist/`
path.

| target | what it holds | runs from |
|---|---|---|
| `tables-java-clean` | google-java-format 1.37.0 `--aosp --dry-run --set-exit-if-changed` over every generated Java file: the 17 table units (`build/tables-generated-java`) and the 8 packet-wire units (`build/java-clean-packet`), counted, refused below 585 | `test-java`, which REQUIRES the formatter (`JAVA_REQUIRE_FORMATTER=1`) |
| `tables-java-compile-all` | every one of those 25 units compiled on its own under `--release 17 -Xlint:all -Werror`, counted, refused below 25 | `tables-java-release` (certify.yml) |
| `tables-java-format-census` | the measurement above: files red and diff lines under the default style and under `--aosp` | by hand |
| `tables-java-clean-formatter-missing-negative-control` | with the formatter pointed at a missing jar, `tables-java-clean` FAILS naming the tool under `SCHEMA_REQUIRE_CORPUS=1`, and SKIPS naming that variable without it | the negative-controls leg, group java |
| `tables-java-clean-drift-negative-control` | the emitter writes one line of `TableBytes.java` a space deeper (`tools/sabotage/table_java_format_drift.go`); `tables-java-clean` must go red naming that file, and the unsabotaged file must be clean first | the negative-controls leg, group java (needs the formatter on the runner) |

**The gate does not skip silently.** Run alone without the formatter,
`tables-java-clean` prints `SKIP` and names `SCHEMA_REQUIRE_CORPUS=1` and
`JAVA_REQUIRE_FORMATTER=1`; with either set, and always under `test-java`, it
fails and names the tool.

## What is not gated

- **The CI step that installs the formatter.** `.github/workflows/` is outside
  this leg's change. Until a workflow puts the pinned jar under `dist/` (or the
  launcher on PATH) on the runners that run `test-java` and the java control
  group, those jobs fail by name on the missing tool, which is the gate doing
  its job and not drift.
- **Fixtures written by hand** (`test/**/Main.java`, `test/conformance/java`)
  are compiled under the lint by their legs but are not format-checked.
- **The default Google style.** Measured above, not gated.
