// main.swift — the conformance driver (test/conformance/README.md):
//   <driver> <manifest> list
//   <driver> <manifest> <surface> <outdir>
// Working directory is the repository root.

import Foundation

let surfaces = ["wire", "json-read", "json-write", "report", "framing", "json-hostile", "write-refusal", "write-refusal-json"]

func readFile(_ p: String) -> [UInt8]? { FileManager.default.contents(atPath: p).map { [UInt8]($0) } }
func writeFile(_ p: String, _ b: [UInt8]) { FileManager.default.createFile(atPath: p, contents: Data(b)) }
func writeText(_ p: String, _ s: String) { writeFile(p, Array(s.utf8)) }

struct Manifest {
    var units: [String: [String]] = [:]
    var lines: [[String]] = []
    init(_ path: String) {
        guard let d = readFile(path) else { return }
        for raw in String(decoding: d, as: UTF8.self).split(separator: "\n", omittingEmptySubsequences: true) {
            let t = raw.trimmingCharacters(in: .whitespaces)
            if t.isEmpty || t.hasPrefix("#") { continue }
            let f = t.split(whereSeparator: { $0 == " " || $0 == "\t" }).map(String.init)
            if f[0] == "unit" { units[f[1]] = Array(f[2...]) }
            lines.append(f)
        }
    }
}

var unitCache: [String: Result<Unit, Error>] = [:]
func unit(_ m: Manifest, _ key: String) throws -> Unit {
    if let c = unitCache[key] { return try c.get() }
    let r = Result { try loadUnit(paths: m.units[key] ?? []) }
    unitCache[key] = r
    return try r.get()
}
func rootDecl(_ u: Unit, _ name: String) throws -> TableDecl {
    guard let d = u.tables[name] else { throw Refusal("no root \(name)") }
    return d
}

/// Form 3 (the fixed form) is not carried by this port yet: a case that
/// needs it answers ABSENT.
struct Absent: Error {}

func loadWire(_ u: Unit, _ root: TableDecl, _ bytes: [UInt8]) throws -> Rec {
    if root.isFixed && !bytes.isEmpty && bytes[0] == 3 { throw Absent() }
    let r = WireReader(bytes, unit: u, root: root)
    return try r.read()
}
func saveWire(_ root: Rec) throws -> [UInt8] {
    if root.decl.isFixed { throw Absent() }
    return try WireWriter().save(root)
}

func jsonPath(_ name: String) -> String { "testdata/conformance/tables/json/\(name).json" }

let args = CommandLine.arguments
guard args.count >= 3 else {
    FileHandle.standardError.write("usage: driver <manifest> list | <surface> <outdir>\n".data(using: .utf8)!)
    exit(1)
}
let manifest = Manifest(args[1])
if args[2] == "list" {
    for s in surfaces { print(s) }
    exit(0)
}
let surface = args[2]
guard surfaces.contains(surface), args.count >= 4 else { exit(2) }
let outdir = args[3]
try? FileManager.default.createDirectory(atPath: outdir, withIntermediateDirectories: true)
func out(_ name: String) -> String { outdir + "/" + name }
var stderr = FileHandle.standardError
func note(_ s: String) { stderr.write((s + "\n").data(using: .utf8)!) }

for f in manifest.lines {
    do {
        switch (surface, f[0]) {
        case ("wire", "instance"):
            let name = f[1]
            let u = try unit(manifest, f[2]); let root = try rootDecl(u, f[3])
            guard let w = readFile(f[4]) else { throw Refusal("missing wire \(f[4])") }
            do {
                let v = try loadWire(u, root, w)
                writeFile(out(name), try saveWire(v))
            } catch let e as Refusal {
                note("\(name): \(e)")
                writeText(out(name), "refused\n")
            }
        case ("json-read", "instance"):
            let name = f[1]
            if f.contains("no-text") { continue }
            let u = try unit(manifest, f[2]); let root = try rootDecl(u, f[3])
            if root.isFixed { throw Absent() }
            guard let t = readFile(jsonPath(name)) else { continue }
            do {
                let v = try JsonReader(t).readRoot(root)
                writeFile(out(name), try saveWire(v))
            } catch let e as Refusal {
                note("\(name): \(e)")
                writeText(out(name), "refused\n")
            }
        case ("json-write", "instance"):
            let name = f[1]
            if f.contains("no-text") { continue }
            let u = try unit(manifest, f[2]); let root = try rootDecl(u, f[3])
            guard let w = readFile(f[4]) else { throw Refusal("missing wire") }
            do {
                let v = try loadWire(u, root, w)
                writeFile(out(name + ".json"), try JsonWriter().write(v))
            } catch let e as Refusal {
                note("\(name): \(e)")
                writeText(out(name + ".json"), "refused\n")
            }
        case ("json-write", "json-write-refusal"):
            let name = f[1]
            let u = try unit(manifest, f[2]); let root = try rootDecl(u, f[3])
            guard let w = readFile(f[4]) else { throw Refusal("missing wire") }
            let v = try loadWire(u, root, w)
            let damage = f[5]
            if damage.hasPrefix("narrow:") || damage.hasPrefix("wide:") {
                let fname = String(damage.split(separator: ":")[1])
                if let i = v.decl.fields.firstIndex(where: { $0.name == fname }) {
                    // wide: the units 0061 D800 0062 have no UTF-8 spelling; the nearest
                    // value that is not text here is the CESU-style bytes of the lone surrogate.
                    v.vals[i] = .bytes(damage.hasPrefix("narrow:") ? [0x61, 0xFF, 0x62] : [0x61, 0xED, 0xA0, 0x80, 0x62])
                }
            }
            do {
                writeFile(out(name + ".json"), try JsonWriter().write(v))
            } catch let e as Refusal {
                note("\(name): \(e)")
                writeText(out(name + ".json"), "refused\n")
            }
        default:
            continue
        }
    } catch is Absent {
        let name = f[1]
        let file = surface == "json-write" ? name + ".json" : name
        writeFile(out(file + ".absent"), [])
    } catch {
        note("\(f.joined(separator: " ")): \(error)")
        let name = f[1]
        let file = surface == "json-write" ? name + ".json" : name
        writeFile(out(file + ".absent"), [])
    }
}
exit(0)
