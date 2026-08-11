#!/usr/bin/env python3
"""v5 EPYC pass analysis: absolute tables, THE RELATIVE TABLE, A/B deltas."""
import csv, statistics, sys, io

CORPUS = ["rigidbody_moving","rigidbody_at_rest","chat","test","inputpacket",
          "shipcreate","ship_shallow","probe_header","probebits","probearray","testdata"]
BATCH = "message_batch"
ORDER = CORPUS + [BATCH]

def load(path):
    """-> dict[(lang,bench,path)] = row dict; skips preamble lines; keeps only Release
    (Debug rows would follow a second preamble — we never run --debug here)."""
    rows = {}
    meta = []
    build = "Release"
    for line in open(path):
        line = line.strip()
        if not line: continue
        if line.startswith("#"):
            meta.append(line)
            if line.startswith("# build:"): build = line.split(":",1)[1].strip()
            continue
        if line.startswith("lang,"): continue
        if build != "Release": continue
        f = line.split(",")
        lang, bench, p = f[0], f[1], f[2]
        rows[(lang,bench,p)] = dict(iters=int(f[3]), bytes=int(f[4]), runs=int(f[5]),
            med=float(f[6]), mn=float(f[7]), mx=float(f[8]), mb=float(f[9]), spread=float(f[10]))
    return rows, meta

def merge(*paths):
    allrows = {}
    for p in paths:
        r,_ = load(p)
        allrows.update(r)
    return allrows

def rel(rows, lang, path_, benches=CORPUS):
    """median over benches of cpp_time/lang_time... wait: time rel to C++ = cpp_rate/lang_rate."""
    ratios = []
    for b in benches:
        c = rows.get(("cpp",b,path_)); l = rows.get((lang,b,path_))
        if not c or not l: return None
        ratios.append(c["med"]/l["med"]*100.0)
    return statistics.median(ratios)

def relative_table(rows):
    out = ["| backend | write | read | batch write | batch read |","|---|---:|---:|---:|---:|",
           "| C++ | 100% | 100% | 100% | 100% |"]
    for lang,name in [("rust","Rust"),("cs","C#"),("go","Go")]:
        w = rel(rows,lang,"write"); r = rel(rows,lang,"read")
        bw = rows[("cpp",BATCH,"write")]["med"]/rows[(lang,BATCH,"write")]["med"]*100
        br = rows[("cpp",BATCH,"read")]["med"]/rows[(lang,BATCH,"read")]["med"]*100
        out.append(f"| {name} | **{w:.0f}%** | **{r:.0f}%** | **{bw:.0f}%** | **{br:.0f}%** |")
    return "\n".join(out)

def absolute_table(rows):
    hdr = "| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |"
    out = [hdr, "|---|---|---:|" + "---:|"*8]
    for b in ORDER:
        for p in ["write","read"]:
            c = rows.get(("cpp",b,p))
            cells = [b, p, str(c["bytes"]) if c else "?"]
            for lang in ["cpp","rust","go","cs"]:
                x = rows.get((lang,b,p))
                cells += [f"{x['med']/1e6:.2f}", f"{x['mb']:.0f}"] if x else ["—","—"]
            out.append("| " + " | ".join(cells) + " |")
    return "\n".join(out)

def ab(rows_a, rows_b, lang="cpp", label_a="A", label_b="B", paths=("write","read")):
    """per-row B/A rate multipliers + spreads: is the move beyond both spreads?"""
    out = [f"| bench | path | {label_a} M msg/s | {label_b} M msg/s | {label_b}/{label_a} | spread {label_a}% | spread {label_b}% |",
           "|---|---|---:|---:|---:|---:|---:|"]
    for b in ORDER:
        for p in paths:
            a = rows_a.get((lang,b,p)); bb = rows_b.get((lang,b,p))
            if not a or not bb: continue
            out.append(f"| {b} | {p} | {a['med']/1e6:.2f} | {bb['med']/1e6:.2f} | **{bb['med']/a['med']:.2f}x** | {a['spread']:.1f} | {bb['spread']:.1f} |")
    return "\n".join(out)

def spreads_over(rows, pct):
    return [(k, v["spread"]) for k,v in sorted(rows.items()) if v["spread"] > pct]

if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "rel":
        print(relative_table(merge(*sys.argv[2:])))
    elif cmd == "abs":
        print(absolute_table(merge(*sys.argv[2:])))
    elif cmd == "ab":
        a,_ = load(sys.argv[2]); b,_ = load(sys.argv[3])
        lang = sys.argv[4] if len(sys.argv)>4 else "cpp"
        la = sys.argv[5] if len(sys.argv)>5 else "A"
        lb = sys.argv[6] if len(sys.argv)>6 else "B"
        print(ab(a,b,lang,la,lb))
    elif cmd == "spreads":
        rows = merge(*sys.argv[3:])
        for k,s in spreads_over(rows, float(sys.argv[2])):
            print(k, s)
