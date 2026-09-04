# Security Policy

schema is a compiler. That makes its threat model two-part, and the two parts
deserve very different weight.

**The generated readers are the security surface that matters.** They run on
packet paths and consume bytes from the network. A bug in the emitted read path
— one that lets a hostile buffer read out of bounds, or lets a declared bound
be exceeded without the read failing — is a serious vulnerability in every
program built from that schema, in nine languages at once. Report these.

**The compiler itself is a build-time tool** that consumes schema files, which
are normally written by you and live in your repository. A crash on a malformed
schema is a bug we want to fix, and the fuzz corpus exists precisely to find
them, but it is not usually a security boundary — the input is yours. It
becomes one if you compile schemas you did not write, which is unusual but not
unheard of (a plugin format, a hosted build service, user-supplied mods).

## Reporting a vulnerability

**Please do not report security issues in public GitHub issues or pull
requests.**

Report privately through either channel:

- **GitHub private vulnerability reporting** (preferred): on this repository,
  go to the **Security** tab → **Report a vulnerability**. This opens a private
  advisory visible only to the maintainers.
- **Email**: glenn@mas-bandwidth.com.

Please include enough detail to reproduce: the affected version or commit, the
schema that triggers it, the target language, and — where possible — the
generated code and a proof-of-concept input. A fuzz crasher (the input file
plus the target name) is ideal.

We will acknowledge your report, keep you updated, and coordinate disclosure
timing with you. We prefer coordinated disclosure and will credit reporters who
wish to be named.

## In scope

**Generated read paths — highest priority.** Any input that a generated reader
accepts when it should have refused, or that causes it to read or write outside
the buffer. Specifically:

- A ranged value outside `[min, max]` that a read accepts.
- An array count, string length or bytes length past its declared bound that is
  not refused before it drives a copy or an allocation.
- An enum value that is not a declared variant being accepted.
- A text payload that is not well formed being accepted ON THE PACKET WIRE:
  malformed UTF-8 in a
  `string(N)`, or in a `wstring(N)` a code unit group above `0xFFFF`, an
  unpaired surrogate, or a zero group among the transmitted units. The rules
  are stated once in SPEC §4.7 and §4.12 and every target owes the same
  verdict, so a target that skips one is a divergence bug as well as an
  acceptance bug. **The TABLE wire enforces the same rules and reports rather
  than stops** (SPEC-TABLES §3, §4): a kind `12` payload that is not
  well-formed UTF-8 or that carries a zero byte, and a kind `33` payload with
  an unpaired surrogate or a zero unit, each leave the field at its declared
  default, count one `malformed`, and let the parent read on past `L`. **A
  table reader that STORES any of them is an acceptance bug on this list**,
  exactly as a packet reader that accepts one is. In scope beside it is the
  framing: a length past a declared bound, and an odd `L` on a kind `33`
  payload or a `*wstring` blob, each refused before it drives a copy.
- Any read that continues past the end of the stream instead of failing.
- Integer overflow in a bit or byte count computed from wire data.
- **Any divergence between the nine languages on the above** — if C++ refuses
  an input and Go accepts it, that is a security bug even if neither is
  memory-unsafe, because deployments mix languages across client and server.

**The compiler**, at lower priority: crashes, hangs and unbounded memory growth
on malformed input, and any path where a schema file causes the compiler to
write outside its output directory or execute anything.

**Miscompilation** — a schema that produces generated code that does not match
what the language specifies, in a way that weakens a check.

## Out of scope

- **Anything transport-level.** schema is not a transport. Replay, spoofing,
  amplification, rate limiting, DoS by flood, and authentication are the
  responsibility of the layer below. Generated code assumes it is handed a
  buffer; how that buffer arrived is not its concern.
- **Confidentiality.** schema does no encryption. The wire is plaintext bits by
  design; encrypt at the transport layer.
- **Write-side validation.** Writes are the caller's responsibility and this is
  documented ([FAQ](FAQ.md), [USAGE.md](USAGE.md)). A program that writes an
  out-of-range value is buggy, but that is not a vulnerability in schema — the
  guarantee is on reads.

## What we do to find these ourselves

- A native Go fuzz harness drives parse → check → generate across the C, C++, C#, Dart, Go, JavaScript and Rust
  backends. Every crasher ever found is committed as a permanent regression
  input and re-run on every push.
- The cross-language corpus generates the same schemas in C, C++, C#, Dart,
  Elixir, Go, Java, JavaScript and Rust
  and compares emitted wire bit for bit against pinned goldens, on Linux and
  macOS, on every push. Divergence between languages fails CI.
- Generated readers are exercised against hand-crafted hostile bytes.
- The TABLES wire is read as untrusted input, exactly as the type wire is,
  and the claim has its own gate: `make tables-wire-fuzz` mutates every pinned
  table wire in the corpus — bit flips, truncation, lengths and counts past
  the body, duplicate ids, kind swaps, node indices out of range — and requires
  the generated read to return, to match an independent oracle (the compiler's
  own engine) on the report and on the decoded value, and never to size a
  region past what the framing can justify, plain and under ASan and UBSan,
  with two negative controls that remove a check from the emitter and require
  the fuzzer to go red (docs/SPEC-TABLES.md §4.2). The cooked and block forms
  are the trusted class — opened at a build version, integrity by signature —
  and their pages say so (§7.4, §19).

None of that is proof. It is why we would rather hear from you.

## Supported versions

The latest tagged release is supported. This is a young project with a small
team; there are no long-term support branches, and a fix will normally land on
`main` and in the next tag rather than being backported.
