package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

const (
	opWire        uint8 = 1 // Form 1 Load + Save == want1
	opRetain      uint8 = 2 // Form 1 LoadRetain + SaveRetain == want1
	opMsg         uint8 = 3 // Form 2 AnnounceRead(aux or own) + LoadMessages + SaveMessages == want1
	opFixed       uint8 = 4 // Form 3 FixedLoad + FixedSave == want1
	opFixedRefuse uint8 = 5 // Form 3 FixedLoad MUST refuse (historical V1 writer-bound violation)
	opJSON        uint8 = 6 // FromJson(in) -> ToJson == want1 AND Save == want2
)

// CorpusCase is one differential test case against a generated schema root.
type CorpusCase struct {
	ID    string
	Pkg   string
	Root  string
	Op    uint8
	Aux   []byte // peer announcement for opMsg (empty => own announcement); batch count in Aux[0]? Let's pass batch count in Want2[0] or read from input[1]+1!
	Input []byte
	Want1 []byte
	Want2 []byte
}

// PackCases serializes cases into a single self-contained binary blob.
func PackCases(cases []CorpusCase) []byte {
	var buf bytes.Buffer
	var u4 [4]byte
	var u2 [2]byte
	putU32 := func(v uint32) {
		binary.LittleEndian.PutUint32(u4[:], v)
		buf.Write(u4[:])
	}
	putStr := func(s string) {
		binary.LittleEndian.PutUint16(u2[:], uint16(len(s)))
		buf.Write(u2[:])
		buf.WriteString(s)
	}
	putBytes := func(b []byte) {
		putU32(uint32(len(b)))
		buf.Write(b)
	}
	putU32(uint32(len(cases)))
	for _, c := range cases {
		putStr(c.ID)
		putStr(c.Pkg)
		putStr(c.Root)
		buf.WriteByte(c.Op)
		putBytes(c.Aux)
		putBytes(c.Input)
		putBytes(c.Want1)
		putBytes(c.Want2)
	}
	return buf.Bytes()
}

// UnpackCases deserializes cases from PackCases bytes.
func UnpackCases(raw []byte) ([]CorpusCase, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("corpus.pack truncated")
	}
	at := 0
	getU32 := func() (uint32, bool) {
		if at+4 > len(raw) {
			return 0, false
		}
		v := binary.LittleEndian.Uint32(raw[at:])
		at += 4
		return v, true
	}
	getStr := func() (string, bool) {
		if at+2 > len(raw) {
			return "", false
		}
		n := int(binary.LittleEndian.Uint16(raw[at:]))
		at += 2
		if at+n > len(raw) {
			return "", false
		}
		s := string(raw[at : at+n])
		at += n
		return s, true
	}
	getBytes := func() ([]byte, bool) {
		n, ok := getU32()
		if !ok || at+int(n) > len(raw) {
			return nil, false
		}
		b := slices.Clone(raw[at : at+int(n)])
		at += int(n)
		return b, true
	}
	cnt, ok := getU32()
	if !ok {
		return nil, fmt.Errorf("bad count")
	}
	out := make([]CorpusCase, 0, cnt)
	for range cnt {
		id, ok1 := getStr()
		pkg, ok2 := getStr()
		root, ok3 := getStr()
		if !ok1 || !ok2 || !ok3 || at >= len(raw) {
			return nil, fmt.Errorf("truncated header")
		}
		op := raw[at]
		at++
		aux, ok4 := getBytes()
		in, ok5 := getBytes()
		w1, ok6 := getBytes()
		w2, ok7 := getBytes()
		if !ok4 || !ok5 || !ok6 || !ok7 {
			return nil, fmt.Errorf("truncated payload")
		}
		out = append(out, CorpusCase{
			ID: id, Pkg: pkg, Root: root, Op: op,
			Aux: aux, Input: in, Want1: w1, Want2: w2,
		})
	}
	return out, nil
}

// BuildCorpus generates and oracle-verifies all Form 1, Form 1 Retain, Form 2,
// Form 3 + schema.lock lineage + historical writer-bound refusal, and JSON
// differential vectors across all pairs.
func BuildCorpus(pairs []*SchemaPair, seed uint64, corpusDir string) ([]CorpusCase, []byte, error) {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return nil, nil, err
	}
	rng := rand.New(rand.NewPCG(seed^0xfeedfacecafebeef, seed+77))
	var cases []CorpusCase

	for _, p := range pairs {
		m1 := tabletext.NewModel(p.UnitV1)
		m2 := tabletext.NewModel(p.UnitV2)

		// 1. Form 1 + JSON on V1 & V2 (VarRoot, FixRoot, BlkRoot)
		for k := range 2 {
			v1Var := randomInstance(rng, m1, m1.Lookup(p.VarRootV1), 0, nil)
			w1, j1, err := verifyOracleVariableAndJSON(m1, v1Var)
			if err != nil {
				return nil, nil, fmt.Errorf("%s v1 var #%d: %w", p.Name, k, err)
			}
			cases = append(cases,
				CorpusCase{ID: fmt.Sprintf("%s:v1_wire_var_%d", p.Name, k), Pkg: p.PkgV1, Root: p.VarRootV1, Op: opWire, Input: w1, Want1: w1},
				CorpusCase{ID: fmt.Sprintf("%s:v1_json_var_%d", p.Name, k), Pkg: p.PkgV1, Root: p.VarRootV1, Op: opJSON, Input: j1, Want1: j1, Want2: w1},
			)

			// Forward Form 1: V1 VarRoot wire decoded by V2 VarRoot[Next]
			inst1As2 := m2.New(m2.Lookup(p.VarRootV2))
			var rep12 tabletext.Report
			ok, err := tablewire.Decode(m2, inst1As2, w1, &rep12)
			if err != nil || !ok || rep12.Refused {
				return nil, nil, fmt.Errorf("%s v1->v2 var #%d decode: ok=%v rep=%+v err=%w", p.Name, k, ok, rep12, err)
			}
			w1As2, err := tablewire.Encode(m2, inst1As2)
			if err != nil {
				return nil, nil, fmt.Errorf("%s v1->v2 var #%d encode: %w", p.Name, k, err)
			}
			cases = append(cases, CorpusCase{
				ID: fmt.Sprintf("%s:v1_to_v2_wire_var_%d", p.Name, k), Pkg: p.PkgV2, Root: p.VarRootV2, Op: opWire, Input: w1, Want1: w1As2,
			})

			v2Var := randomInstance(rng, m2, m2.Lookup(p.VarRootV2), 0, nil)
			w2, j2, err := verifyOracleVariableAndJSON(m2, v2Var)
			if err != nil {
				return nil, nil, fmt.Errorf("%s v2 var #%d: %w", p.Name, k, err)
			}
			cases = append(cases,
				CorpusCase{ID: fmt.Sprintf("%s:v2_wire_var_%d", p.Name, k), Pkg: p.PkgV2, Root: p.VarRootV2, Op: opWire, Input: w2, Want1: w2},
				CorpusCase{ID: fmt.Sprintf("%s:v2_json_var_%d", p.Name, k), Pkg: p.PkgV2, Root: p.VarRootV2, Op: opJSON, Input: j2, Want1: j2, Want2: w2},
			)
		}

		// Backward Form 1 + Form 1 Retain: V2 VarRoot[Next] wire (with V1-compatible narrowed scalar magnitudes + V2-only fields/arms/variants/pointers!) decoded by V1 VarRoot!
		v2Compat := randomInstance(rng, m2, m2.Lookup(p.VarRootV2), 0, p.UnitV1)
		w2Compat, _, err := verifyOracleVariableAndJSON(m2, v2Compat)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2 compat var: %w", p.Name, err)
		}
		inst2As1 := m1.New(m1.Lookup(p.VarRootV1))
		var rep21 tabletext.Report
		ok, err := tablewire.Decode(m1, inst2As1, w2Compat, &rep21)
		if err != nil || !ok || rep21.Refused {
			return nil, nil, fmt.Errorf("%s v2->v1 var decode: ok=%v rep=%+v err=%w", p.Name, ok, rep21, err)
		}
		w2As1Plain, err := tablewire.Encode(m1, inst2As1)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2->v1 var encode: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v2_to_v1_wire_var", p.Name), Pkg: p.PkgV1, Root: p.VarRootV1, Op: opWire, Input: w2Compat, Want1: w2As1Plain,
		})

		inst2As1Ret := m1.New(m1.Lookup(p.VarRootV1))
		ret := tablewire.Retain{Capacity: 262144, IdCapacity: 8192}
		var repRet tabletext.Report
		ok, err = tablewire.DecodeRetain(m1, inst2As1Ret, w2Compat, &ret, &repRet)
		if err != nil || !ok || repRet.Refused {
			return nil, nil, fmt.Errorf("%s v2->v1 retain decode: ok=%v rep=%+v err=%w", p.Name, ok, repRet, err)
		}
		var repRetSave tabletext.Report
		w2As1Ret, err := tablewire.EncodeRetain(m1, inst2As1Ret, &ret, &repRetSave)
		if err != nil || repRetSave.Refused {
			return nil, nil, fmt.Errorf("%s v2->v1 retain encode: rep=%+v err=%w", p.Name, repRetSave, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v2_to_v1_retain_var", p.Name), Pkg: p.PkgV1, Root: p.VarRootV1, Op: opRetain, Input: w2Compat, Want1: w2As1Ret,
		})

		// 2. Fixed roots: Form 1 (EncodeVariable) + Form 3 (EncodeFixed) + JSON + BlkRoot
		v1Fix := randomInstance(rng, m1, m1.Lookup(p.FixRootV1), 0, nil)
		_, w1Fix3, j1Fix, err := verifyOracleFixedRoot(m1, v1Fix)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v1 fix: %w", p.Name, err)
		}
		cases = append(cases,
			CorpusCase{ID: fmt.Sprintf("%s:v1_fixed_same", p.Name), Pkg: p.PkgV1, Root: p.FixRootV1, Op: opFixed, Input: w1Fix3, Want1: w1Fix3},
			CorpusCase{ID: fmt.Sprintf("%s:v1_json_fix", p.Name), Pkg: p.PkgV1, Root: p.FixRootV1, Op: opJSON, Input: j1Fix, Want1: j1Fix},
		)

		v2Fix := randomInstance(rng, m2, m2.Lookup(p.FixRootV2), 0, nil)
		_, w2Fix3, j2Fix, err := verifyOracleFixedRoot(m2, v2Fix)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2 fix: %w", p.Name, err)
		}
		cases = append(cases,
			CorpusCase{ID: fmt.Sprintf("%s:v2_fixed_same", p.Name), Pkg: p.PkgV2, Root: p.FixRootV2, Op: opFixed, Input: w2Fix3, Want1: w2Fix3},
			CorpusCase{ID: fmt.Sprintf("%s:v2_json_fix", p.Name), Pkg: p.PkgV2, Root: p.FixRootV2, Op: opJSON, Input: j2Fix, Want1: j2Fix},
		)

		// BlkRoot Form 3 + JSON on V2 (ensures BlkRoot generated code is runtime-exercised too!)
		v2Blk := randomInstance(rng, m2, m2.Lookup(p.BlkRootV2), 0, nil)
		_, w2Blk3, j2Blk, err := verifyOracleFixedRoot(m2, v2Blk)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2 blk: %w", p.Name, err)
		}
		cases = append(cases,
			CorpusCase{ID: fmt.Sprintf("%s:v2_fixed_blk", p.Name), Pkg: p.PkgV2, Root: p.BlkRootV2, Op: opFixed, Input: w2Blk3, Want1: w2Blk3},
			CorpusCase{ID: fmt.Sprintf("%s:v2_json_blk", p.Name), Pkg: p.PkgV2, Root: p.BlkRootV2, Op: opJSON, Input: j2Blk, Want1: j2Blk},
		)

		// 3. Form 3 Lineage Widening (V1 Form-3 image -> V2 FixedLoad via schema.lock!)
		instFix1As2 := m2.New(m2.Lookup(p.FixRootV2))
		var repFixLin tabletext.Report
		ok, err = tablewire.Decode(m2, instFix1As2, w1Fix3, &repFixLin)
		if err != nil || !ok || repFixLin.Refused {
			return nil, nil, fmt.Errorf("%s v1->v2 Form 3 lineage decode failed: ok=%v rep=%+v err=%w", p.Name, ok, repFixLin, err)
		}
		w1FixAs2, err := tablewire.EncodeFixed(m2, instFix1As2)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v1->v2 Form 3 lineage encode: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v1_to_v2_fixed_lineage", p.Name), Pkg: p.PkgV2, Root: p.FixRootV2, Op: opFixed, Input: w1Fix3, Want1: w1FixAs2,
		})

		// 4. Form 3 Historical V1 Writer-Bound Refusal!
		// Find exact Form-3 record body start via differential encoding of two legal V1 values,
		// or len(w1Fix3) - TableFixedTypeBytes if there is no trailer! Let's check with a positive control!
		bodyBytes := int(ir.TableFixedTypeBytes(p.UnitV1.Tables[p.FixRootV1]))
		hdrBytes := len(w1Fix3) - bodyBytes
		if hdrBytes < 16 {
			return nil, nil, fmt.Errorf("%s unexpected Form-3 header size %d", p.Name, hdrBytes)
		}
		for bIdx, bs := range p.BreachSpecs {
			// Positive control: patching bs.GoodLE at off MUST be accepted!
			pos := slices.Clone(w1Fix3)
			off := hdrBytes + bs.RawOffset
			copy(pos[off:off+bs.RawWidth], bs.GoodLE)
			posProbe := m2.New(m2.Lookup(p.FixRootV2))
			var posRep tabletext.Report
			pok, perr := tablewire.Decode(m2, posProbe, pos, &posRep)
			if perr != nil || !pok || posRep.Refused {
				return nil, nil, fmt.Errorf("%s positive control #%d (%s) at off=%d refused (ok=%v rep=%+v err=%w) — check Form-3 record hash", p.Name, bIdx, bs.FieldV1, off, pok, posRep, perr)
			}

			mut := slices.Clone(w1Fix3)
			copy(mut[off:off+bs.RawWidth], bs.BadLE)
			probe := m2.New(m2.Lookup(p.FixRootV2))
			var bRep tabletext.Report
			bok, berr := tablewire.Decode(m2, probe, mut, &bRep)
			if berr == nil && bok && !bRep.Refused {
				return nil, nil, fmt.Errorf("%s breach #%d (%s) was unexpectedly ACCEPTED by tablewire with SchemaLock=true", p.Name, bIdx, bs.FieldV1)
			}
			cases = append(cases, CorpusCase{
				ID:  fmt.Sprintf("%s:v1_to_v2_fixed_breach_%d_%s", p.Name, bIdx, bs.FieldV1),
				Pkg: p.PkgV2, Root: p.FixRootV2, Op: opFixedRefuse, Input: mut,
			})
		}

		// 5. Form 2 Messages (same-version multi-body batches + V1<->V2 cross-version batches!)
		v1BatchInsts := []*tabletext.Instance{
			m1.New(m1.Lookup(p.FixRootV1)), // all-default body!
			v1Fix,
			randomInstance(rng, m1, m1.Lookup(p.FixRootV1), 0, nil),
		}
		v1BatchBytes, err := tablewire.EncodeMessages(m1, v1BatchInsts)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v1 EncodeMessages: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v1_msg_same", p.Name), Pkg: p.PkgV1, Root: p.FixRootV1, Op: opMsg, Input: v1BatchBytes, Want1: v1BatchBytes,
		})

		v2BatchInsts := []*tabletext.Instance{
			m2.New(m2.Lookup(p.FixRootV2)),
			v2Fix,
			randomInstance(rng, m2, m2.Lookup(p.FixRootV2), 0, nil),
		}
		v2BatchBytes, err := tablewire.EncodeMessages(m2, v2BatchInsts)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2 EncodeMessages: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v2_msg_same", p.Name), Pkg: p.PkgV2, Root: p.FixRootV2, Op: opMsg, Input: v2BatchBytes, Want1: v2BatchBytes,
		})

		// V1 -> V2 cross-version Form-2 batch!
		annV1 := tablewire.Announce(p.UnitV1)
		var voc12 tablewire.Vocabulary
		var repAnn12 tabletext.Report
		if err := voc12.AnnounceRead(annV1, &repAnn12); err != nil || repAnn12.Refused {
			return nil, nil, fmt.Errorf("%s v1 AnnounceRead: %w", p.Name, err)
		}
		v1ForV2MsgInsts := []*tabletext.Instance{
			m1.New(m1.Lookup(p.FixRootV1)),
			randomInstance(rng, m1, m1.Lookup(p.FixRootV1), 0, p.UnitV2),
			randomInstance(rng, m1, m1.Lookup(p.FixRootV1), 0, p.UnitV2),
		}
		v1ForV2MsgBytes, err := tablewire.EncodeMessages(m1, v1ForV2MsgInsts)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v1ForV2 EncodeMessages: %w", p.Name, err)
		}
		dec12 := []*tabletext.Instance{
			m2.New(m2.Lookup(p.FixRootV2)),
			m2.New(m2.Lookup(p.FixRootV2)),
			m2.New(m2.Lookup(p.FixRootV2)),
		}
		var repMsg12 tabletext.Report
		n12, ok12, err := tablewire.DecodeMessages(m2, dec12, v1ForV2MsgBytes, &voc12, &repMsg12)
		if err != nil || !ok12 || n12 != 3 || repMsg12.Refused {
			return nil, nil, fmt.Errorf("%s v1->v2 DecodeMessages: n=%d ok=%v rep=%+v err=%w", p.Name, n12, ok12, repMsg12, err)
		}
		wantMsg12, err := tablewire.EncodeMessages(m2, dec12)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v1->v2 EncodeMessages: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v1_to_v2_msg", p.Name), Pkg: p.PkgV2, Root: p.FixRootV2, Op: opMsg, Aux: annV1, Input: v1ForV2MsgBytes, Want1: wantMsg12,
		})

		// V2 -> V1 cross-version Form-2 batch (using V1-compatible narrowed magnitudes + V2 added fields/arms/variants)!
		v2FixCompat := []*tabletext.Instance{
			randomInstance(rng, m2, m2.Lookup(p.FixRootV2), 0, p.UnitV1),
			randomInstance(rng, m2, m2.Lookup(p.FixRootV2), 0, p.UnitV1),
		}
		v2CompatBatch, err := tablewire.EncodeMessages(m2, v2FixCompat)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2 compat EncodeMessages: %w", p.Name, err)
		}
		annV2 := tablewire.Announce(p.UnitV2)
		var voc21 tablewire.Vocabulary
		var repAnn21 tabletext.Report
		if err := voc21.AnnounceRead(annV2, &repAnn21); err != nil || repAnn21.Refused {
			return nil, nil, fmt.Errorf("%s v2 AnnounceRead: %w", p.Name, err)
		}
		dec21 := []*tabletext.Instance{
			m1.New(m1.Lookup(p.FixRootV1)),
			m1.New(m1.Lookup(p.FixRootV1)),
		}
		var repMsg21 tabletext.Report
		n21, ok21, err := tablewire.DecodeMessages(m1, dec21, v2CompatBatch, &voc21, &repMsg21)
		if err != nil || !ok21 || n21 != 2 || repMsg21.Refused {
			return nil, nil, fmt.Errorf("%s v2->v1 DecodeMessages: n=%d ok=%v rep=%+v err=%w", p.Name, n21, ok21, repMsg21, err)
		}
		wantMsg21, err := tablewire.EncodeMessages(m1, dec21)
		if err != nil {
			return nil, nil, fmt.Errorf("%s v2->v1 EncodeMessages: %w", p.Name, err)
		}
		cases = append(cases, CorpusCase{
			ID: fmt.Sprintf("%s:v2_to_v1_msg", p.Name), Pkg: p.PkgV1, Root: p.FixRootV1, Op: opMsg, Aux: annV2, Input: v2CompatBatch, Want1: wantMsg21,
		})
	}

	packed := PackCases(cases)
	if err := os.WriteFile(filepath.Join(corpusDir, "corpus.pack"), packed, 0o644); err != nil {
		return nil, nil, err
	}
	return cases, packed, nil
}

func verifyOracleVariableAndJSON(m *tabletext.Model, inst *tabletext.Instance) (wireBytes, jsonBytes []byte, err error) {
	wireBytes, err = tablewire.Encode(m, inst)
	if err != nil {
		return nil, nil, err
	}
	jsonBytes, err = m.Write(inst)
	if err != nil {
		return nil, nil, err
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	// Verify JSON -> Instance -> Wire & JSON canonicality!
	chk := m.New(inst.Def)
	var rep tabletext.Report
	if ok := m.Read(chk, jsonBytes, &rep); !ok || rep.Refused {
		return nil, nil, fmt.Errorf("oracle Read failed: ok=%v rep=%+v\n%s", ok, rep, jsonBytes)
	}
	w2, err := tablewire.Encode(m, chk)
	if err != nil {
		return nil, nil, fmt.Errorf("oracle re-Encode: %w", err)
	}
	if !bytes.Equal(w2, wireBytes) {
		// Re-canonicalize from chk (e.g. quantized float rounding!)
		wireBytes = w2
		jsonBytes, err = m.Write(chk)
		if err != nil {
			return nil, nil, err
		}
		if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
			jsonBytes = append(jsonBytes, '\n')
		}
	}
	return wireBytes, jsonBytes, nil
}

func verifyOracleFixedRoot(m *tabletext.Model, inst *tabletext.Instance) (varWire, fixWire, jsonBytes []byte, err error) {
	jsonBytes, err = m.Write(inst)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	chk := m.New(inst.Def)
	var rep tabletext.Report
	if ok := m.Read(chk, jsonBytes, &rep); !ok || rep.Refused {
		return nil, nil, nil, fmt.Errorf("oracle fixed Read failed: ok=%v rep=%+v\n%s", ok, rep, jsonBytes)
	}
	// Re-serialize through Form 2 / Form 1 / Form 3 so quantized floats settle on their exact wire step!
	msgs, err := tablewire.EncodeMessages(m, []*tabletext.Instance{chk})
	if err != nil {
		return nil, nil, nil, err
	}
	var voc tablewire.Vocabulary
	var arep tabletext.Report
	if err := voc.AnnounceRead(tablewire.Announce(m.Unit), &arep); err != nil {
		return nil, nil, nil, err
	}
	settled := []*tabletext.Instance{m.New(inst.Def)}
	var mrep tabletext.Report
	if _, ok, err := tablewire.DecodeMessages(m, settled, msgs, &voc, &mrep); err != nil || !ok {
		return nil, nil, nil, fmt.Errorf("settle DecodeMessages: ok=%v err=%w", ok, err)
	}
	chk = settled[0]
	varWire, err = tablewire.EncodeVariable(m, chk)
	if err != nil {
		return nil, nil, nil, err
	}
	fixWire, err = tablewire.EncodeFixed(m, chk)
	if err != nil {
		return nil, nil, nil, err
	}
	jsonBytes, err = m.Write(chk)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	return varWire, fixWire, jsonBytes, nil
}

func findPeerStruct(peer *ir.Unit, st *ir.Struct) *ir.Struct {
	if peer == nil || st == nil {
		return nil
	}
	want := st.WireName()
	for _, s := range peer.Tables {
		if s.WireName() == want {
			return s
		}
	}
	for _, s := range peer.Structs {
		if s.WireName() == want {
			return s
		}
	}
	return nil
}

func findPeerField(pst *ir.Struct, f *ir.Field) *ir.Field {
	if pst == nil || f == nil {
		return nil
	}
	id := ir.TableFieldWireId(f)
	for _, pf := range pst.Fields {
		if ir.TableFieldWireId(pf) == id {
			return pf
		}
	}
	return nil
}

// randomInstance populates a valid random Instance of st. When peer != nil
// (V2 -> V1 backward compatibility), fields whose wire kind widened relative
// to peer stay at their declared default (eliding on Form 1/2), while V2-only
// fields, variants, arms, pointers, and same-kind fields ARE populated!
func randomInstance(rng *rand.Rand, m *tabletext.Model, st *ir.Struct, depth int, peer *ir.Unit) *tabletext.Instance {
	inst := m.New(st)
	pst := findPeerStruct(peer, st)
	v1Compat := peer != nil
	var sharedNode *tabletext.Instance
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if pf := findPeerField(pst, f); pf != nil {
			fk, rfk := ir.TableFieldKind(f), ir.TableFieldKind(pf)
			ek, rek := ir.TableElemKind(f), ir.TableElemKind(pf)
			kindOK := fk == rfk || ir.TableKindWidens(fk, rfk)
			elemOK := ek == rek || ir.TableKindWidens(ek, rek)
			if !kindOK || !elemOK {
				continue
			}
		}
		// 25% of the time leave field at its declared default to exercise default elision!
		if rng.IntN(4) == 0 && !f.Type.Optional {
			continue
		}
		if f.Type.Optional {
			fv.Present = rng.IntN(3) != 0 // 67% present (and 25% of present left at default inside!)
			if !fv.Present {
				continue
			}
			if rng.IntN(4) == 0 {
				continue // Present == true with all-default inner value!
			}
		}
		if f.IsMap() {
			populateMap(rng, m, fv, depth, peer)
			continue
		}
		if f.KeyEnum != "" {
			for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
				if rng.IntN(2) == 0 {
					populateElemCell(rng, m, f, &fv.Elems[slot], depth+1, peer)
				}
			}
			continue
		}
		if f.Array == ir.ArrayFixed {
			for idx := range int(f.ArrayBound) {
				populateElemCell(rng, m, f, &fv.Elems[idx], depth+1, peer)
			}
			continue
		}
		if f.Array == ir.ArrayCounted {
			maxN := int(f.ArrayBound)
			if v1Compat && maxN > 2 {
				maxN = 2
			}
			cnt := rng.IntN(maxN + 1)
			fv.Count = cnt
			for len(fv.Elems) < cnt {
				fv.Elems = append(fv.Elems, tabletext.Cell{})
			}
			for idx := range cnt {
				populateElemCell(rng, m, f, &fv.Elems[idx], depth+1, peer)
			}
			continue
		}
		if f.IsList() {
			cnt := rng.IntN(3) + 1
			fv.Count = cnt
			fv.Elems = make([]tabletext.Cell, cnt)
			for idx := range cnt {
				populateElemCell(rng, m, f, &fv.Elems[idx], depth+1, peer)
			}
			continue
		}
		if f.Type.Pointer {
			if f.Type.Kind == ir.TString {
				fv.Cell.Blob = &tabletext.Blob{Data: []byte(randomUTF8String(rng, 20))}
			} else if f.Type.Kind == ir.TBytes {
				fv.Cell.Blob = &tabletext.Blob{Data: randomBytes(rng, 10)}
			} else if target, ok := f.Type.Ref.(*ir.Struct); ok && depth < 2 {
				if sharedNode != nil && sharedNode.Def == target && rng.IntN(2) == 0 {
					fv.Cell.Node = sharedNode // Shared DAG pointer!
				} else {
					node := randomInstance(rng, m, target, depth+1, peer)
					fv.Cell.Node = node
					if sharedNode == nil {
						sharedNode = node
					}
				}
			}
			continue
		}
		populateElemCell(rng, m, f, &fv.Cell, depth+1, peer)
	}
	return inst
}

func populateMap(rng *rand.Rand, m *tabletext.Model, fv *tabletext.Field, depth int, peer *ir.Unit) {
	entryDef := fv.Def.MapEntry
	if entryDef == nil {
		return
	}
	keyField := entryDef.Fields[0]
	if keyField.Type.Kind == ir.TString {
		keys := []string{"", "alpha", "Z-key", "\uff61", "\U0001f600", "quote\"key"}
		n := rng.IntN(3) + 1
		perm := rng.Perm(len(keys))[:n]
		// Sort keys in canonical UTF-8 byte order so fv.Entries is canonical!
		picked := make([]string, n)
		for i, idx := range perm {
			picked[i] = keys[idx]
		}
		slices.Sort(picked)
		fv.Entries = make([]tabletext.Cell, 0, n)
		for _, k := range picked {
			if keyField.Type.Size > 0 && len(k) > int(keyField.Type.Size) {
				continue
			}
			eInst := randomInstance(rng, m, entryDef, depth+1, peer)
			eInst.Fields[0].Cell.Str = []byte(k)
			eInst.Fields[0].Count = len(k)
			fv.Entries = append(fv.Entries, tabletext.Cell{Tab: eInst})
		}
	} else if keyField.Type.Kind == ir.TInt && keyField.Type.Signed {
		keys := []int64{-9007199254740993, -1, 0, 42, 9007199254740993}
		n := rng.IntN(3) + 1
		perm := rng.Perm(len(keys))[:n]
		picked := make([]int64, n)
		for i, idx := range perm {
			picked[i] = keys[idx]
		}
		slices.Sort(picked)
		fv.Entries = make([]tabletext.Cell, 0, n)
		for _, k := range picked {
			eInst := randomInstance(rng, m, entryDef, depth+1, peer)
			eInst.Fields[0].Cell.I = k
			fv.Entries = append(fv.Entries, tabletext.Cell{Tab: eInst})
		}
	}
}

func populateElemCell(rng *rand.Rand, m *tabletext.Model, f *ir.Field, cell *tabletext.Cell, depth int, peer *ir.Unit) {
	v1Compat := peer != nil
	switch f.Type.Kind {
	case ir.TBool:
		cell.B = rng.IntN(2) == 1
	case ir.TString:
		maxN := int(f.Type.Size)
		if maxN <= 0 || maxN > 24 {
			maxN = 20
		}
		s := randomUTF8String(rng, maxN)
		cell.Str = []byte(s)
	case ir.TBytes:
		maxN := int(f.Type.Size)
		if maxN <= 0 || maxN > 12 {
			maxN = 8
		}
		cell.Str = randomBytes(rng, rng.IntN(maxN+1))
	case ir.TFloat32, ir.TFloat64:
		if f.HasFloatRange {
			// Pick an exact quarter step inside [FMin, FMax]
			steps := int((f.FMax - f.FMin) * 4)
			if steps > 0 {
				cell.F = f.FMin + float64(rng.IntN(steps+1))*0.25
			} else {
				cell.F = f.FMin
			}
		} else {
			vals := []float64{-12.5, -0.25, 0.0, 1.5, 42.75, 1024.5}
			cell.F = vals[rng.IntN(len(vals))]
		}
	case ir.TBits:
		w := f.Type.Width
		if v1Compat && w > 5 {
			w = 5
		}
		if w >= 64 {
			cell.U = (uint64(1) << 63) | uint64(rng.IntN(1000))
		} else {
			cell.U = uint64(rng.Int64N(int64(1) << w))
		}
	case ir.TFixed:
		var raw int64
		if f.Type.Signed {
			raw = int64(rng.IntN(161)-80) * 16
		} else {
			raw = int64(rng.IntN(71)+10) * 16
		}
		cell.Wide = big.NewInt(raw)
	case ir.TInt:
		populateIntCell(rng, f, cell, v1Compat)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			cell.U = uint64(rng.IntN(len(ref.Variants) + 1))
		case *ir.Flags:
			nbits := len(ref.Variants)
			if peer != nil {
				if pfl := peer.Flags[ref.Name]; pfl != nil && len(pfl.Variants) < nbits {
					nbits = len(pfl.Variants)
				}
			}
			mask := uint64(0)
			for b := range nbits {
				if rng.IntN(2) == 1 {
					mask |= 1 << b
				}
			}
			cell.U = mask
		case *ir.Struct:
			if depth < 4 {
				cell.Tab = randomInstance(rng, m, ref, depth+1, peer)
			} else {
				cell.Tab = m.New(ref)
			}
		case *ir.Union:
			if depth >= 4 {
				cell.U = 0
				return
			}
			tag := rng.IntN(len(ref.Variants) + 1)
			if peer != nil && tag > 0 {
				arm := ref.Variants[tag-1]
				pun := peer.Unions[ref.Name]
				if pun == nil {
					pun = peer.TableUnions[ref.Name]
				}
				if pun != nil && arm.F != nil {
					aid := ir.TableWireId(arm.WireName())
					for _, pv := range pun.Variants {
						if pv.F != nil && ir.TableWireId(pv.WireName()) == aid {
							fk, rfk := ir.TableScalarKind(arm.F), ir.TableScalarKind(pv.F)
							if fk != rfk && !ir.TableKindWidens(fk, rfk) {
								tag = len(ref.Variants) // switch to V2-only arm!
							}
							break
						}
					}
				}
			}
			cell.U = uint64(tag)
			if tag == 0 {
				return
			}
			arm := ref.Variants[tag-1]
			if arm.Void() {
				return
			}
			if arm.Body() {
				cell.Tab = randomInstance(rng, m, arm.Ref, depth+1, peer)
				return
			}
			af := m.NewArm(arm)
			populateElemCell(rng, m, arm.F, &af.Cell, depth+1, peer)
			if arm.F.Type.Kind == ir.TString || arm.F.Type.Kind == ir.TBytes {
				af.Count = len(af.Cell.Str)
			}
			cell.Arm = af
		}
	}
}

func populateIntCell(rng *rand.Rand, f *ir.Field, cell *tabletext.Cell, v1Compat bool) {
	if f.Type.Width == 128 {
		if v1Compat {
			if f.Type.Signed {
				cell.Wide = big.NewInt(int64(rng.IntN(21) - 10))
			} else {
				cell.Wide = big.NewInt(int64(rng.IntN(30) + 10))
			}
			return
		}
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			span := new(big.Int).Sub(f.IntMax, f.IntMin)
			if !span.IsInt64() {
				// True >64-bit 128-bit range: exercise values beyond +/- 2^64!
				hi := big.NewInt(int64(rng.IntN(5) - 2))
				if hi.Sign() == 0 {
					hi = big.NewInt(-2)
				}
				v := new(big.Int).Lsh(hi, 65)
				v.Add(v, big.NewInt(int64(rng.IntN(100000)-50000)))
				if v.Cmp(f.IntMin) >= 0 && v.Cmp(f.IntMax) <= 0 {
					cell.Wide = v
					return
				}
				cell.Wide = new(big.Int).Set(f.IntMin)
				return
			}
			lim := min(span.Int64()+1, 50000)
			if lim <= 0 {
				lim = 1
			}
			off := big.NewInt(rng.Int64N(lim))
			cell.Wide = new(big.Int).Add(f.IntMin, off)
			return
		}
		if f.Type.Signed {
			// Negative 128-bit value < -2^64!
			hi := big.NewInt(-3)
			v := new(big.Int).Lsh(hi, 64)
			v.Add(v, big.NewInt(int64(rng.IntN(100000))))
			cell.Wide = v
		} else {
			// Unsigned 128-bit value > 2^64!
			hi := big.NewInt(2)
			v := new(big.Int).Lsh(hi, 64)
			v.Add(v, big.NewInt(int64(rng.IntN(100000))))
			cell.Wide = v
		}
		return
	}

	if v1Compat {
		if f.Type.Signed {
			lo, hi := int64(-15), int64(15)
			if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
				lo = max(lo, f.IntMin.Int64())
				hi = min(hi, f.IntMax.Int64())
			}
			cell.I = lo + rng.Int64N(hi-lo+1)
		} else {
			lo, hi := uint64(10), uint64(40)
			if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
				lo = max(lo, f.IntMin.Uint64())
				hi = min(hi, f.IntMax.Uint64())
			}
			if hi < lo {
				lo = hi
			}
			cell.U = lo + uint64(rng.Int64N(int64(hi-lo)+1))
		}
		return
	}

	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		if f.Type.Signed {
			lo, hi := f.IntMin.Int64(), f.IntMax.Int64()
			cell.I = lo + rng.Int64N(hi-lo+1)
		} else {
			lo, hi := f.IntMin.Uint64(), f.IntMax.Uint64()
			cell.U = lo + uint64(rng.Int64N(int64(min(hi-lo, math.MaxInt32))+1))
		}
		return
	}

	if f.Type.Signed {
		switch f.Type.Width {
		case 8:
			cell.I = int64(rng.IntN(256) - 128)
		case 16:
			cell.I = int64(rng.IntN(65536) - 32768)
		case 32:
			cell.I = int64(rng.IntN(2000000) - 1000000)
		default:
			// Exercise > 2^53 and negative > 2^53!
			choices := []int64{-9007199254740995, -42, 0, 9007199254740993, -9223372036854775807}
			cell.I = choices[rng.IntN(len(choices))]
		}
	} else {
		switch f.Type.Width {
		case 8:
			cell.U = uint64(rng.IntN(256))
		case 16:
			cell.U = uint64(rng.IntN(65536))
		case 32:
			cell.U = uint64(rng.Int64N(1 << 31))
		default:
			choices := []uint64{0, 42, 9007199254740993, 9223372036854775809, 18446744073709551610}
			cell.U = choices[rng.IntN(len(choices))]
		}
	}
}

func randomUTF8String(rng *rand.Rand, maxBytes int) string {
	samples := []string{
		"",
		`a"b\c`,
		"αβγ",
		`hi "Ω" 1`,
		"tab\tline\n",
		"😀!",
		"plain-ascii",
	}
	s := samples[rng.IntN(len(samples))]
	for len(s) > maxBytes {
		s = s[:len(s)-1]
	}
	// Ensure we didn't slice mid-UTF-8 rune
	for len(s) > 0 && (s[len(s)-1]&0xc0) == 0x80 {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] >= 0x80 {
		s = s[:len(s)-1]
	}
	return s
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.IntN(256))
	}
	return b
}
