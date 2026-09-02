// cookgen writes a SYNTHETIC COOK of a chosen size, for the gate §7 states as
// its scale requirement: *"Assume we have say, 100mbs or many gigabytes of data
// in Assets.bin at some point."* / *"We would want this to be fast :)"* — open
// time FLAT across a 1 MB, a 100 MB and a 1 GB cook.
//
// It exists because a gate needs inputs a person can regenerate rather than a
// gigabyte in the tree, and because the CONSUMER of those inputs is the C++
// `Open` this repo has not written yet: the files are the cross-implementation
// fixture, produced by the Go cooker from the page's own layout rules.
//
// It writes STREAMING and holds O(1) memory, which is why a gigabyte is
// generable on a laptop: the region is a Scene root followed by a chain of
// ListNode records at a constant pitch, so every offset and every self-relative
// delta is arithmetic rather than a graph in memory. The instance model is
// deliberately not used — thirty-three million Go instances is not a fixture,
// it is a swap file.
//
//	go run ./test/cookgen --bytes 1048576 --out build/cook/1mb.cook
//	go run ./test/cookgen --bytes 104857600 --byte-order big --out build/cook/100mb-be.cook
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func main() {
	bytesWanted := flag.Int64("bytes", 1<<20, "approximate size of the cook to write")
	out := flag.String("out", "", "file to write")
	unitDir := flag.String("unit", "tables/pointers", "the unit the synthetic root is declared in")
	rootName := flag.String("root", "Scene", "the root table")
	chainName := flag.String("chain", "ListNode", "the table the chain is made of")
	refField := flag.String("ref", "head", "the root's field that names the chain's first node")
	nextField := flag.String("next", "next", "the chain table's own forward reference")
	byteOrder := flag.String("byte-order", "little", "little or big")
	flag.Parse()
	if *out == "" {
		fatal("cookgen needs --out <file>")
	}
	var ord binary.ByteOrder = binary.LittleEndian
	orderWord := tablecook.ByteOrderLittle
	switch *byteOrder {
	case "little":
	case "big":
		ord, orderWord = binary.BigEndian, tablecook.ByteOrderBig
	default:
		fatal("--byte-order takes little or big, not %q", *byteOrder)
	}

	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{*unitDir})
	if err != nil {
		fatal("%v", err)
	}
	u, err := c.Load(paths)
	if err != nil {
		fatal("%v", err)
	}
	root, chain := u.Tables[*rootName], u.Tables[*chainName]
	if root == nil || chain == nil {
		fatal("%s declares no table %s or no table %s", *unitDir, *rootName, *chainName)
	}
	rootLayout := ir.RecordLayout(u, root)
	chainLayout := ir.RecordLayout(u, chain)

	rootRef := rootLayout.FieldByName(*refField)
	chainRef := chainLayout.FieldByName(*nextField)
	if rootRef == nil || chainRef == nil {
		fatal("--ref %s or --next %s names no field", *refField, *nextField)
	}

	align := ir.RegionAlignOf(rootLayout.Align, chainLayout.Align)
	chainBase := alignUp(rootLayout.Size, chainLayout.Align)
	// how many nodes fit in the size asked for, over the region alone
	nodes := max((*bytesWanted-chainBase)/chainLayout.Size, 1)
	dataLength := alignUp(chainBase+nodes*chainLayout.Size, align)
	attribLength := (nodes + 1) * 16

	f, err := os.Create(*out)
	if err != nil {
		fatal("%v", err)
	}
	w := bufio.NewWriterSize(f, 1<<20)

	// ---- the header ----
	header := make([]byte, tablecook.HeaderBytes)
	ord.PutUint64(header[0:], tablecook.Magic)
	ord.PutUint64(header[8:], ir.BuildVersion(u))
	ord.PutUint64(header[16:], orderWord)
	ord.PutUint64(header[24:], uint64(dataLength))
	ord.PutUint64(header[32:], uint64(attribLength))
	ord.PutUint64(header[40:], uint64(align))
	dataOffset := alignUp(tablecook.HeaderBytes, align)
	must(w.Write(header))
	must(w.Write(make([]byte, dataOffset-tablecook.HeaderBytes)))

	// ---- the region: the root, then the chain ----
	//
	// Every byte outside a written slot is ZERO, which is a legal value for
	// every companion the check bounds — a used length of zero, a count of
	// zero, an enum ordinal of None, a union tag of None — so the file this
	// writes is one `schema cook-check` passes rather than merely one that
	// opens.
	rootRec := make([]byte, rootLayout.Size)
	// the root's reference names the first chain node: the SELF-RELATIVE delta
	// from the slot's own address (§6.3)
	ord.PutUint32(rootRec[rootRef.Offset:], uint32(int32(chainBase-rootRef.Offset)))
	must(w.Write(rootRec))
	must(w.Write(make([]byte, chainBase-rootLayout.Size)))

	chainRec := make([]byte, chainLayout.Size)
	// the pitch is constant, so the forward delta is the SAME NUMBER in every
	// record: one node on, less the slot's own position inside the record
	forward := chainLayout.Size - chainRef.Offset
	ord.PutUint32(chainRec[chainRef.Offset:], uint32(int32(forward)))
	for range nodes - 1 {
		must(w.Write(chainRec))
	}
	// the last node's reference is NULL, which in a region is a delta of zero
	ord.PutUint32(chainRec[chainRef.Offset:], 0)
	must(w.Write(chainRec))
	must(w.Write(make([]byte, dataLength-(chainBase+nodes*chainLayout.Size))))

	// ---- the attribution: one entry per node, in index order ----
	entry := make([]byte, 16)
	ord.PutUint64(entry[0:], 0)
	ord.PutUint64(entry[8:], ir.TableTypeId(root.Name))
	must(w.Write(entry))
	chainTypeId := ir.TableTypeId(chain.Name)
	for i := range nodes {
		ord.PutUint64(entry[0:], uint64(chainBase+i*chainLayout.Size))
		ord.PutUint64(entry[8:], chainTypeId)
		must(w.Write(entry))
	}

	if err := w.Flush(); err != nil {
		fatal("%v", err)
	}
	if err := f.Close(); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("wrote %s: %d bytes, %s-endian, %d nodes (%s root + %d %s), build version 0x%016x\n",
		*out, dataOffset+dataLength+attribLength, *byteOrder, nodes+1, root.Name, nodes, chain.Name, ir.BuildVersion(u))
}

func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

func must(_ int, err error) {
	if err != nil {
		fatal("%v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cookgen: "+format+"\n", args...)
	os.Exit(1)
}
