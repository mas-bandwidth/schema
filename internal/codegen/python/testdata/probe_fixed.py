# The fixed-form probe (internal/codegen/python/fixedform_test.go): loads the
# file `file` through <package>.<module>.<Root>.load_fixed and prints
#   ok <records> w<widened>
# then every value in the reference manifest's spelling, then the bytes
# save_fixed writes back as x<hex>; or, on a refusal,
#   refused <reason> <layout hash or ->
# The arguments are one line in `args`: <package> <module> <Root>.
import os
import struct
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import importlib

pkg, mod, root = open("args").read().split()
m = importlib.import_module(pkg + "." + mod)
fx = importlib.import_module(pkg + "._schema.fixed")
rt = importlib.import_module(pkg + "._schema")
cls = getattr(m, root)
shape = getattr(m, "_s_fs_" + root)()
data = open("file", "rb").read()
report = fx.Report()
try:
    values = cls.load_fixed(data, report)
except rt.Refused as err:
    h = getattr(err, "layout_hash", None)
    print("refused %s %s" % (err.reason, "#%016x" % h if h is not None else "-"))
    print("detail=%s" % err)
    sys.exit(0)

lines = []


def emit(k, v):
    lines.append("%s=%s" % (k, v))


def dump(node, v, path):
    if isinstance(node, fx.Table):
        for f in node.fields:
            fv = getattr(v, f.attr)
            p = path + "." + f.attr
            if f.optional:
                emit(p + "_present", "1" if fv is not None else "0")
                if fv is None:
                    continue
            dump(f.node, fv, p)
    elif isinstance(node, fx.Bool):
        emit(path, "1" if v else "0")
    elif isinstance(node, fx.Int):
        emit(path, ("raw=%d" if 20 <= node.kind <= 29 else "%d") % v)
    elif isinstance(node, fx.Float):
        if node.kind == 10:
            emit(path, "0x%08X" % struct.unpack("<I", struct.pack("<f", v))[0])
        else:
            emit(path, "0x%016X" % struct.unpack("<Q", struct.pack("<d", v))[0])
    elif isinstance(node, fx.Enum):
        emit(path, "%d" % v)
    elif isinstance(node, fx.Text):
        if node.kind == 12:
            emit(path, '"%s"' % v)
            emit(path + "_length", "%d" % len(v.encode("utf-8")))
        else:
            emit(path + "_length", "%d" % (len(v.encode("utf-16-le")) // 2))
    elif isinstance(node, fx.Bytes):
        for i, b in enumerate(v):
            emit("%s[%d]" % (path, i), "%d" % b)
        emit(path + "_length", "%d" % len(v))
    elif isinstance(node, fx.Array):
        if node.counted:
            emit(path + "_count", "%d" % len(v))
        for i, e in enumerate(v):
            dump(node.elem, e, "%s[%d]" % (path, i))
    elif isinstance(node, fx.Keyed):
        items = v.slots() if node.wrap else v
        for i, e in enumerate(items):
            dump(node.elem, e, "%s[%d]" % (path, i))
    elif isinstance(node, fx.Union):
        emit(path + ".type", "%d" % v.type)
        if v.type > 0:
            arm = node.arms[v.type - 1]
            if arm.node is not None:
                dump(arm.node, getattr(v, arm.attr), path + "." + arm.attr)
    else:
        raise TypeError(node)


for i, v in enumerate(values):
    dump(shape, v, "r%d" % i)
print("ok %d w%d" % (len(values), report.widened))
for line in lines:
    print(line)
print("x" + cls.save_fixed(values).hex())
