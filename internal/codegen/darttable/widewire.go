package darttable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Both injected members take widths in BYTES: 1, 2, 4, 8 or 16. The wire
// holds raw scaled integers, little-endian throughout (SPEC-TABLES §3).
const wideWireWriterMembers = `
  void putBigInt(BigInt raw,int width) {
    var bits=raw.toUnsigned(width*8);
    for(var i=0;i<width;i++){putByte((bits&BigInt.from(255)).toInt());bits=bits>>8;}
  }
`

// The helper owns the failed-preflight report flag. It consumes no prefix
// on failure; callers receiving null must not attempt to place a value.
const wideWireReaderMembers = `
  BigInt? readBigInt(int width,bool signed) {
    if(!has(width)){report.malformed=true;return null;}
    var raw=BigInt.zero;
    for(var i=0;i<width;i++){raw|=BigInt.from(readByte())<<(i*8);}
    return signed?raw.toSigned(width*8):raw;
  }
`

func wideWireStorageType(f *ir.Field) string {
	if f.Type.Width == 128 {
		return "BigInt"
	}
	return "int"
}

func wideWireBigLiteral(raw *big.Int) string {
	if raw == nil || raw.Sign() == 0 {
		return "BigInt.zero"
	}
	return fmt.Sprintf("BigInt.parse('%s')", raw.String())
}

func wideWireDefault(f *ir.Field) string {
	// ir.Field.DefInt and check.go's fixed-default lowering already hold RAW
	// units*2^F; tabletext.Model.fieldDefault uses that same integer unchanged.
	var raw *big.Int
	if f.HasDefault {
		raw = f.DefInt
	}
	if f.Type.Width == 128 {
		return wideWireBigLiteral(raw)
	}
	return dartBoundLiteral(raw)
}

// Declared bounds are on the raw scale, unlike TFixed's whole-unit IntMin/Max.
func wideWireRawBounds(f *ir.Field) (lo, hi string, ok bool) {
	low, high, bounded := ir.TableRawRange(f)
	if !bounded {
		return "", "", false
	}
	return wideWireBigLiteral(low), wideWireBigLiteral(high), true
}

func wideWireRawValue(f *ir.Field, expr string) string {
	if f.Type.Width == 128 {
		return expr
	}
	value := "BigInt.from(" + expr + ")"
	if !f.Type.Signed && f.Type.Width == 64 {
		value += ".toUnsigned(64)"
	}
	return value
}

func wideWireStorageValue(f *ir.Field, rawExpr string) string {
	if f.Type.Width == 128 {
		return rawExpr
	}
	// An unsigned 64-bit value uses the native int's signed bit pattern.
	return "(" + rawExpr + ").toSigned(64).toInt()"
}
