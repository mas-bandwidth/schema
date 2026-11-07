// harness.lobster — the BENCH-STANDARD discipline both Lobster legs share
// (bench/README.md "Runner contract"; bench/tables/README.md "The protocol";
// docs/PORT-LOBSTER.md, THE HARNESS). Shape-blind: timing, statistics, the
// corpus_id fold, the variant files and the output lines. Nothing here names a
// generated type.
//
// THE OUTPUT GRAMMAR. A Lobster program has one output stream, and the pinned
// toolchain prints its own compiler warnings there too, so every line this
// harness prints carries a tag the leg's shell wrapper routes:
//
//   csv <row>   a CSV v2 row (BENCH-STANDARD §5.1): stdout, under --csv
//   log <text>  the human table and the diagnostics: stderr
//
// and the wrapper REFUSES any other line, so a compiler warning in a
// generated module or in this harness fails the leg instead of riding past it.

// the 64 rotating records of the committed variant corpus (§2.7)
let bench_num_variants = 64

// the ceiling --iterations accepts, every leg's: whole 64-record rotations
// that also fit a 32-bit long
let bench_max_iterations = 2147483584

class BenchOptions:
    csv:bool = false
    gate:bool = false
    quick:bool = false
    round:bool = false
    iterations:int = 0
    wire_dir:string = ""
    variant_dir:string = ""
    bad:string = ""

var bench_failed = false
let bench_rows:[string] = []
let bench_golden_names:[string] = []
let bench_golden_bytes:[string] = []

def bench_log(text:string):
    print("log " + text)

def bench_fail(row:string, what:string):
    bench_log("FAILED: " + row + ": " + what)
    bench_failed = true

// bench_options reads the runner's arguments (everything after `--`, after the
// wrapper's own `--root <dir>`): run.sh's --csv, --round K and --quick, and
// the paired driver's --gate, --iterations, --wire-dir and --variant-dir.
def bench_options(args:[string]) -> BenchOptions:
    let o = BenchOptions {}
    var root = ""
    var i = 0
    while i < length(args):
        let a = args[i]
        if a == "--csv":
            o.csv = true
        elif a == "--gate":
            o.gate = true
        elif a == "--quick":
            o.quick = true
        elif a == "--round" and i + 1 < length(args):
            i += 1
            let k, ok = string_to_int(args[i])
            if not ok or k < 0:
                o.bad = "--round takes a non-negative integer, got " + args[i]
            o.round = true
        elif a == "--iterations" and i + 1 < length(args):
            i += 1
            let n, ok = string_to_int(args[i])
            if not ok or n <= 0 or n > bench_max_iterations or n % bench_num_variants != 0:
                o.bad = "--iterations requires a positive multiple of 64 up to 2147483584"
            o.iterations = n
        elif a == "--root" and i + 1 < length(args):
            i += 1
            root = args[i]
        elif a == "--wire-dir" and i + 1 < length(args):
            i += 1
            o.wire_dir = args[i]
        elif a == "--variant-dir" and i + 1 < length(args):
            i += 1
            o.variant_dir = args[i]
        else:
            o.bad = "usage: [--csv] [--gate] [--round K] [--quick] [--iterations N] [--wire-dir <dir>] [--variant-dir <dir>] (got " + a + ")"
        i += 1
    // a relative read_file resolves against the PROGRAM's directory, so every
    // corpus path is made absolute against the repository root the wrapper
    // passes
    if o.wire_dir == "":
        o.wire_dir = root + "/testdata/wire"
    elif not bench_absolute(o.wire_dir):
        o.wire_dir = root + "/" + o.wire_dir
    if o.variant_dir == "":
        o.variant_dir = root + "/bench/corpus/variants"
    elif not bench_absolute(o.variant_dir):
        o.variant_dir = root + "/" + o.variant_dir
    return o

def bench_absolute(path:string) -> bool:
    return length(path) > 0 and (path[0] == '/' or (length(path) > 2 and path[1] == ':'))

// load reads one corpus file and records it for corpus_id (§1.6); nil, with
// the failure named, when it is absent
def bench_load(dir:string, name:string, row:string) -> string?:
    let data = read_file(dir + "/" + name)
    if not data:
        bench_fail(row, "missing " + dir + "/" + name + " — run from the schema repo root, or pass --wire-dir/--variant-dir")
        return nil
    push(bench_golden_names, name)
    push(bench_golden_bytes, data)
    return data

// ---------------------------------------------------------------------------
// corpus_id (§1.6): FNV-1a-64 over the goldens this run loaded, each as its
// basename, a 0x00 byte and its contents, in sorted basename order, as 16
// lowercase hex digits. The 64-bit product is carried in two 32-bit limbs so
// no step overflows a signed int (the --cpp build is C++).
// ---------------------------------------------------------------------------

class BenchFnv:
    hi:int = 0xcbf29ce4
    lo:int = 0x84222325

def bench_fnv_byte(h:BenchFnv, b:int):
    let lo = h.lo ^ (b & 0xff)
    let p = lo * 0x1b3
    let carry = p >> 32
    h.hi = (h.hi * 0x1b3 + lo * 0x100 + carry) & 0xffffffff
    h.lo = p & 0xffffffff

def bench_fnv_string(h:BenchFnv, s:string):
    for(s) c:
        bench_fnv_byte(h, c)

def bench_hex32(v:int) -> string:
    return lowercase(number_to_string(v, 16, 8))

def bench_corpus_id() -> string:
    let order:[int] = []
    for(length(bench_golden_names)) i:
        push(order, i)
    // insertion sort by basename: the C++ leg's std::map order
    for(length(order)) i:
        var j = i
        while j > 0 and bench_golden_names[order[j]] < bench_golden_names[order[j - 1]]:
            let t = order[j]
            order[j] = order[j - 1]
            order[j - 1] = t
            j -= 1
    let h = BenchFnv {}
    for(order) k:
        bench_fnv_string(h, bench_golden_names[k])
        bench_fnv_byte(h, 0)
        bench_fnv_string(h, bench_golden_bytes[k])
    return bench_hex32(h.hi) + bench_hex32(h.lo)

// ---------------------------------------------------------------------------
// the variant corpus: 64 records, read as exact strings and as copies padded
// with the eight bytes of slack a serialize read stream needs
// ---------------------------------------------------------------------------

class BenchVariants:
    exact:[string] = []
    padded:[string] = []
    record:int = 0

def bench_pad8(data:string) -> string:
    let n = length(data)
    let padded = ensure_size(string_with_capacity(n + 8), n + 8, 0)
    write_substring(padded, 0, data, false)
    return padded

// bench_split_variants cuts a fixed-stride file into its 64 records: a file that does
// not divide evenly is a refusal (the records are one length by construction)
def bench_split_variants(packed:string, row:string) -> BenchVariants?:
    if length(packed) == 0 or length(packed) % bench_num_variants != 0:
        bench_fail(row, "variant data is " + string(length(packed)) + " bytes, not a multiple of 64 records — refusing to bench data whose stride is not the record size")
        return nil
    let v = BenchVariants { record: length(packed) / bench_num_variants }
    for(bench_num_variants) k:
        let r = substring(packed, k * v.record, v.record)
        push(v.exact, r)
        push(v.padded, bench_pad8(r))
    return v

// ---------------------------------------------------------------------------
// statistics and rows
// ---------------------------------------------------------------------------

class BenchStats:
    median:float
    min:float
    max:float
    spread:float

def bench_stats(rates:[float]) -> BenchStats:
    let s:[float] = []
    for(rates) r:
        push(s, r)
    for(length(s)) i:
        var j = i
        while j > 0 and s[j] < s[j - 1]:
            let t = s[j]
            s[j] = s[j - 1]
            s[j - 1] = t
            j -= 1
    let n = length(s)
    let median = s[n / 2]
    return BenchStats { median: median, min: s[0], max: s[n - 1], spread: (s[n - 1] - s[0]) / median * 100.0 }

// fixed renders x with d decimals, rounded half away from zero, as every
// leg's %.Nf does for the positive rates this prints
def bench_fixed(x:float, d:int) -> string:
    var scale = 1
    for(d): scale *= 10
    let n = round(x * float(scale))
    if d == 0:
        return string(n)
    let whole = n / scale
    let part = n % scale
    return string(whole) + "." + number_to_string(part, 10, d)

def bench_pad_right(s:string, w:int) -> string:
    var out = s
    while length(out) < w:
        out += " "
    return out

def bench_pad_left(s:string, w:int) -> string:
    var out = s
    while length(out) < w:
        out = " " + out
    return out

// report prints one (bench, path) to the human table and records its row
def bench_report(lang:string, bench:string, path:string, iters:int, bytes_per_op:int, rates:[float]):
    let s = bench_stats(rates)
    let mbps = s.median * float(bytes_per_op) / (1024.0 * 1024.0)
    bench_log(bench_pad_right(bench, 18) + " " + bench_pad_right(path, 11) + bench_pad_left(bench_fixed(s.median / 1000000.0, 3), 10) + " M msg/s " +
        bench_pad_left(bench_fixed(mbps, 1), 10) + " MB/s   (min " + bench_fixed(s.min / 1000000.0, 3) + ", max " +
        bench_fixed(s.max / 1000000.0, 3) + ", spread " + bench_fixed(s.spread, 1) + "%)")
    push(bench_rows, lang + "," + bench + "," + path + "," + string(iters) + "," + string(bytes_per_op) + "," + string(length(rates)) + "," +
        bench_fixed(s.median, 0) + "," + bench_fixed(s.min, 0) + "," + bench_fixed(s.max, 0) + "," + bench_fixed(mbps, 2) + "," + bench_fixed(s.spread, 2))

// derived prints READ = round-trip minus write: informational, never a row,
// because a derived number in a CSV is divided as if it had been measured (§2.9)
def bench_derived(bench:string, write_rates:[float], trip_rates:[float]):
    let w = bench_stats(write_rates)
    let rt = bench_stats(trip_rates)
    let read_time = 1.0 / rt.median - 1.0 / w.median
    if read_time > 0.0:
        bench_log(bench_pad_right(bench, 18) + " " + bench_pad_right("read", 11) + bench_pad_left(bench_fixed(0.000001 / read_time, 3), 10) +
            " M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)")

// finish emits the rows (none from a failing run, §1.5) and sets the exit code
def bench_finish(o:BenchOptions, family_suffix:string):
    let id = bench_corpus_id()
    if bench_failed:
        bench_log("refusing to emit CSV rows from a failing run")
        bench_log("BENCH FAILED (corpus_id " + id + ")")
        set_exit_code(1)
        return
    if o.csv and not o.gate:
        print("csv lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline")
        for(bench_rows) row:
            print("csv " + row + "," + id + "," + family_suffix)
    if o.gate:
        bench_log("OK (gate only, corpus_id " + id + ")")
    else:
        bench_log("OK (corpus_id " + id + ")")
