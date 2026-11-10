// The packet carrier's JavaScript driver (test/conformance/packet/carrier.go
// states the contract): write|read <codec> <in> <out>. The generated module
// sits beside it; the serialize.js runtime is named by SERIALIZE_JS_INDEX.
import { readFileSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import * as seed from './Seed.js';

const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);

function fail(msg) {
  process.stderr.write('driver: ' + msg + '\n');
  process.exit(1);
}

function writeSeedT(f) {
  const v = new seed.T();
  let at = 0;
  if (f.length < 1) return null;
  const tag = f[at++];
  if (tag === 1) {
    if (at >= f.length) return null;
    v.u.type = seed.UType.A;
    v.u.a = new seed.Small();
    v.u.a.v = f[at++];
  } else if (tag === 0) {
    v.u.type = seed.UType.None;
  } else {
    return null;
  }
  for (let i = 0; i < 4; i++) {
    if (at >= f.length) return null;
    v.data[i] = f[at++];
  }
  if (at !== f.length) return null;
  const ws = new WriteStream(new Uint8Array(seed.TMaxBytes + 8));
  if (!seed.WriteT(ws, v)) return null;
  ws.flush();
  return Uint8Array.from(ws.data());
}

function readSeedT(p) {
  const v = new seed.T();
  if (!seed.ReadT(new ReadStream(p), v)) return null;
  const out = [v.u.type];
  if (v.u.type === seed.UType.A) out.push(v.u.a.v);
  for (let i = 0; i < 4; i++) out.push(v.data[i]);
  return out;
}

const codecs = { 'union-before-bytes': [writeSeedT, readSeedT] };

const [verb, codec, inPath, outPath] = process.argv.slice(2);
if (outPath === undefined) fail('usage: driver write|read <codec> <in> <out>');
if (!(codec in codecs)) fail('no codec ' + codec);
const [writer, reader] = codecs[codec];
const lines = readFileSync(inPath, 'utf8').split('\n');
if (lines[lines.length - 1] === '') lines.pop();
let out = '';
for (const line of lines) {
  if (verb === 'write') {
    const fields = line.split(/\s+/).filter((t) => t !== '').map((t) => {
      const n = Number(t);
      if (!Number.isSafeInteger(n)) fail('not an integer: ' + t);
      return n;
    });
    const p = writer(fields);
    out += p === null ? 'refused\n' : Buffer.from(p).toString('hex') + '\n';
  } else if (verb === 'read') {
    if (!/^([0-9a-f]{2})*$/.test(line)) fail('bad hex line');
    const fields = reader(Uint8Array.from(Buffer.from(line, 'hex')));
    out += fields === null ? 'refused\n' : 'ok' + fields.map((n) => ' ' + n).join('') + '\n';
  } else {
    fail('no verb ' + verb);
  }
}
writeFileSync(outPath, out);
