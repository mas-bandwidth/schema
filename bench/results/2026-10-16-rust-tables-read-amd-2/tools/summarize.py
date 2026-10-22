# summarize.py CSV REF OTHER... : per-binary medians (M rec/s) with spread, the
# DERIVED read (1/(1/round_trip - 1/write), as the runner derives it), and per-round
# ratios REF/OTHER, median [min, max]. Read is derived, so its ratio is labelled so.
import csv, statistics as st, sys
rows = list(csv.DictReader(open(sys.argv[1])))
ref, others = sys.argv[2], sys.argv[3:]
d = {}
for r in rows:
    d.setdefault(r["binary"], {}).setdefault(int(r["round"]), {})[r["path"]] = float(r["median_msgs_per_sec"])
for b in d:
    for k, v in d[b].items():
        v["read"] = 1.0 / (1.0 / v["round_trip"] - 1.0 / v["write"])
def stats(xs):
    m = st.median(xs); return m, min(xs), max(xs), 100 * (max(xs) - min(xs)) / m
print("binary            path        median M/s  ns/rec   min     max    spread%")
for b in [ref] + others:
    for p in ("write", "round_trip", "read"):
        m, lo, hi, sp = stats([v[p] for v in d[b].values()])
        print(f"{b:18s}{p:11s}{m/1e6:9.3f} {1e9/m:8.1f} {lo/1e6:7.3f} {hi/1e6:7.3f} {sp:6.1f}")
print()
for o in others:
    for p in ("write", "round_trip", "read"):
        rs = [d[ref][k][p] / d[o][k][p] for k in sorted(d[ref]) if k in d[o]]
        m, lo, hi, _ = stats(rs)
        print(f"{ref}/{o} {p:11s} {m:.3f} [{lo:.3f}, {hi:.3f}]  n={len(rs)}")
