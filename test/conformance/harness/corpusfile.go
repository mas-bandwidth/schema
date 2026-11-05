// The conformance corpus's FILE form is the VARIABLE form.
//
// Every wire golden under testdata/wire/tables is form 1, including the goldens
// of tables the schema declares `fixed`. The fixed form is its own corpus and
// its own surfaces; the engine's declared-root rule ("a reader that expects a
// fixed table refuses every other form") is held by internal/tablewire's own
// tests. These helpers let the harness read and write the pinned corpus as the
// bytes it actually is, rather than refusing every form-1 golden at a declared
// fixed root.
package main

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fileDecode reads one corpus file wire (form 1, or form 3 through the fixed
// reader).
func fileDecode(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return tablewire.DecodeFileForm(m, inst, data, report)
}

// fileDecodeRegion is the file form's region read over the corpus.
func fileDecodeRegion(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return tablewire.DecodeRegionFileForm(m, inst, data, report)
}

// fileEncode writes one corpus file wire in form 1.
func fileEncode(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	return tablewire.EncodeFileForm(m, inst)
}

// fileUnpackOneFile is [compiler.Compiler.UnpackOneFile] over the corpus file
// form.
func fileUnpackOneFile(c *compiler.Compiler, u *ir.Unit, root string, wire []byte, dir string) (compiler.TableReport, error) {
	return withVariableRoot(u.Tables[root], func() (compiler.TableReport, error) {
		return c.UnpackOneFile(u, root, wire, dir)
	})
}

// filePack is [compiler.Compiler.Pack] over the corpus file form.
func filePack(c *compiler.Compiler, u *ir.Unit, root, dir string) ([]byte, []string, compiler.TableReport, error) {
	type result struct {
		wire    []byte
		skipped []string
		report  compiler.TableReport
		err     error
	}
	r, _ := withVariableRoot(u.Tables[root], func() (result, error) {
		wire, skipped, report, err := c.Pack(u, root, dir)
		return result{wire, skipped, report, err}, err
	})
	return r.wire, r.skipped, r.report, r.err
}

// fileCook is [compiler.Compiler.Cook] over the corpus file form.
func fileCook(c *compiler.Compiler, u *ir.Unit, root string, wire []byte, opts compiler.CookOptions) ([]byte, compiler.CookReport, compiler.TableReport, error) {
	type result struct {
		cooked []byte
		report compiler.CookReport
		tr     compiler.TableReport
		err    error
	}
	r, _ := withVariableRoot(u.Tables[root], func() (result, error) {
		cooked, rep, tr, err := c.Cook(u, root, wire, opts)
		return result{cooked, rep, tr, err}, err
	})
	return r.cooked, r.report, r.tr, r.err
}

// withVariableRoot runs fn with the table declared variable, then restores the
// declaration. The corpus is read and written serially (t.Chdir forbids a
// parallel test), so this is the one flag the file surfaces move.
func withVariableRoot[T any](st *ir.Struct, fn func() (T, error)) (T, error) {
	fixed := st.FixedDeclared
	st.FixedDeclared = false
	defer func() { st.FixedDeclared = fixed }()
	return fn()
}
