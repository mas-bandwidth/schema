// Golden-gated Squirrel packet bench over the committed 64-record corpus.
// Shape-blind: names the generated root once (`rootName`) and nothing else.
local writeErr = function(s) {
  foreach (c in s) ::stderr.writen(c & 0xff, 'b');
  ::stderr.flush();
};

// sq exits 0 after an uncaught throw, so a failed gate ("bench failed") used
// to end the process with status 0 and the gate recipe stayed green. The
// program is one function; its throw is caught at the bottom and the script
// returns 1, which is sq's exit status.
local main = function(argv) {
if (_floatsize_ != 8) {
  throw "bench-squirrel requires 8-byte floats (-DSQUSEDOUBLE)";
}

local NUM_VARIANTS = 64;
local MAX_RUNS = 7;
local DEFAULT_ITERS = 3200;
local rootName = "BenchMixed";

if (argv.len() < 1 || argv[0].len() >= 2 && argv[0].slice(0, 2) == "--") {
  writeErr("usage: sq main.nut <gen-dir> [--csv] [--gate] [--quick] [--round K] [--iterations N] [--wire-dir D] [--variant-dir D]\n");
  throw "bench failed";
}
local genDir = argv[0];
local csv = false;
local gate = false;
local quick = false;
local numRuns = MAX_RUNS;
local itersOverride = 0;
local wireDir = "testdata/wire";
local variantDir = "bench/corpus/variants";

for (local i = 1; i < argv.len(); i++) {
  local a = argv[i];
  if (a == "--csv") csv = true;
  else if (a == "--gate") gate = true;
  else if (a == "--quick") quick = true;
  else if (a == "--wire-dir" && i + 1 < argv.len()) wireDir = argv[++i];
  else if (a == "--variant-dir" && i + 1 < argv.len()) variantDir = argv[++i];
  else if (a == "--round" && i + 1 < argv.len()) {
    numRuns = 1;
    i++;
  } else if (a == "--iterations" && i + 1 < argv.len()) {
    itersOverride = argv[++i].tointeger();
    if (itersOverride <= 0 || itersOverride % NUM_VARIANTS != 0) {
      writeErr("--iterations expects positive multiple of 64\n");
      throw "bench failed";
    }
  } else {
    writeErr("unknown argument " + a + "\n");
    throw "bench failed";
  }
}
if (quick && numRuns == MAX_RUNS) numRuns = 3;

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local bench = dofile(genDir + "/schema_module.nut", true).require("Bench");
local Ctor = bench[rootName];
local readFn = bench["Read" + rootName];
local writeFn = bench["Write" + rootName];

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local blobsEqual = function(a, b) {
  if (a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) {
    if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  }
  return true;
};

local sliceBlob = function(src, start, len) {
  local out = blob(len);
  for (local i = 0; i < len; i++) out.writen(src[start + i] & 0xff, 'b');
  out.seek(0);
  return out;
};

local golden = readBytes(wireDir + "/bench_mixed.bin");
local packed = readBytes(variantDir + "/bench_mixed.variants.bin");
if (packed.len() == 0 || packed.len() % NUM_VARIANTS != 0) {
  writeErr("FAILED: bench_mixed: invalid variant corpus size\n");
  throw "bench failed";
}
local record = packed.len() / NUM_VARIANTS;
local variants = [];
for (local k = 0; k < NUM_VARIANTS; k++) {
  variants.append(sliceBlob(packed, k * record, record));
}

if (!blobsEqual(variants[0], golden)) {
  writeErr("WIRE GOLDEN MISMATCH: bench_mixed\n");
  throw "bench failed";
}

local encodeOne = function(v) {
  local ws = serialize.WriteStream(blob(4096));
  if (!writeFn(ws, v)) return null;
  ws.flush();
  return ws.data();
};

local values = [];
for (local k = 0; k < NUM_VARIANTS; k++) {
  local v = Ctor();
  variants[k].seek(0);
  local rs = serialize.ReadStream(variants[k]);
  if (!readFn(rs, v)) {
    writeErr("FAILED: bench_mixed: decode of variant " + k + " failed\n");
    throw "bench failed";
  }
  local out = encodeOne(v);
  if (out == null || !blobsEqual(out, variants[k])) {
    writeErr("FAILED: bench_mixed: variant " + k + " round-trip bytes differ\n");
    throw "bench failed";
  }
  values.append(v);
}

local reused = Ctor();
for (local step = 0; step < 2 * NUM_VARIANTS; step++) {
  local k = step % NUM_VARIANTS;
  variants[k].seek(0);
  local rs = serialize.ReadStream(variants[k]);
  if (!readFn(rs, reused)) {
    writeErr("FAILED: bench_mixed: reused decode failed\n");
    throw "bench failed";
  }
  local out = encodeOne(reused);
  if (out == null || !blobsEqual(out, variants[k])) {
    writeErr("FAILED: bench_mixed: reused round-trip bytes differ\n");
    throw "bench failed";
  }
}

if (gate) {
  writeErr("OK (gate only)\n");
  return;
}

local iters = itersOverride > 0 ? itersOverride : (quick ? 320 : DEFAULT_ITERS);
local sink = 0;
foreach (pathName in ["write", "round_trip"]) {
  local rates = [];
  local holder = Ctor();
  for (local run = 0; run <= numRuns; run++) {
    local t0 = clock();
    for (local idx = 0; idx < iters; idx++) {
      local k = idx % NUM_VARIANTS;
      local v = values[k];
      if (pathName == "round_trip") {
        variants[k].seek(0);
        local rs = serialize.ReadStream(variants[k]);
        if (!readFn(rs, holder)) throw "bench failed";
        v = holder;
      }
      local out = encodeOne(v);
      if (out == null || out.len() != record) throw "bench failed";
      sink += out.len();
    }
    local elapsed = clock() - t0;
    if (elapsed <= 0.0) elapsed = 1e-6;
    if (run > 0) rates.append(iters.tofloat() / elapsed);
  }
  rates.sort();
  local med = rates[rates.len() / 2];
  local lo = rates[0];
  local hi = rates[rates.len() - 1];
  local line = format(
    "squirrel,bench_mixed,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,0000000000000000,gen,pkg,always,default,unknown\n",
    pathName, iters, record, numRuns, med, lo, hi, med * record / 1048576.0, (hi - lo) / med * 100.0
  );
  if (csv) {
    if (pathName == "write") {
      print("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
    }
    print(line);
  } else {
    writeErr(line);
  }
}
writeErr("OK (sink " + sink + ")\n");
};

local status = 0;
try {
  main(vargv);
} catch (e) {
  writeErr("FAILED: " + e + "\n");
  status = 1;
}
return status;
