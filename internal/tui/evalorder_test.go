package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Go leaves the order of evaluating a variable relative to a call in the same
// expression unspecified. So in
//
//	return m, m.openInstall()
//
// where m is a copy and openInstall changes it through its pointer, whether the
// copy that is returned has the change depends on the compiler. It works with
// gc today; this test keeps it that way by finding such statements, so the call
// is made first and its result kept in a variable.
func TestNoReturnStatementReturnsACopyAlongsideACallThatChangesIt(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
	}

	// methods that change their receiver through a pointer
	pointerMethods := map[string]map[string]bool{"Model": {}, "Setup": {}}
	for _, f := range parsed {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if id, ok := star.X.(*ast.Ident); ok && pointerMethods[id.Name] != nil {
					pointerMethods[id.Name][fn.Name.Name] = true
				}
			}
		}
	}

	var found []string
	var scan func(body ast.Node, copies map[string]string)
	scan = func(body ast.Node, copies map[string]string) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				// a closure's own parameters shadow the copies around it (a *Model
				// parameter is not a copy), and bring their own
				inner := map[string]string{}
				for k, v := range copies {
					inner[k] = v
				}
				for _, p := range n.Type.Params.List {
					for _, name := range p.Names {
						delete(inner, name.Name)
					}
					noteCopies(inner, p.Names, p.Type, pointerMethods)
				}
				scan(n.Body, inner)
				return false
			case *ast.ReturnStmt:
				for variable, typ := range copies {
					returnsCopy := false
					for _, r := range n.Results {
						if id, ok := r.(*ast.Ident); ok && id.Name == variable {
							returnsCopy = true
						}
					}
					if !returnsCopy {
						continue
					}
					for _, r := range n.Results {
						ast.Inspect(r, func(c ast.Node) bool {
							if _, ok := c.(*ast.FuncLit); ok {
								return false // runs later, on whatever it is given then
							}
							call, ok := c.(*ast.CallExpr)
							if !ok {
								return true
							}
							sel, ok := call.Fun.(*ast.SelectorExpr)
							if !ok {
								return true
							}
							if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == variable && pointerMethods[typ][sel.Sel.Name] {
								pos := fset.Position(n.Pos())
								found = append(found, pos.Filename+":"+strconv.Itoa(pos.Line)+": return "+variable+" together with "+variable+"."+sel.Sel.Name+"()")
							}
							return true
						})
					}
				}
			}
			return true
		})
	}
	for _, f := range parsed {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// the copies in play: value receivers and parameters of type Model or Setup
			copies := map[string]string{}
			if fn.Recv != nil {
				for _, r := range fn.Recv.List {
					noteCopies(copies, r.Names, r.Type, pointerMethods)
				}
			}
			for _, p := range fn.Type.Params.List {
				noteCopies(copies, p.Names, p.Type, pointerMethods)
			}
			if len(copies) > 0 {
				scan(fn.Body, copies)
			}
		}
	}
	sort.Strings(found)
	for _, f := range found {
		t.Error(f + ": make the call first, keep what it returns, then return")
	}
}

// noteCopies records the names declared with type typ as copies, if typ is a
// type (not a pointer to it) that has methods changing it through a pointer.
func noteCopies(copies map[string]string, names []*ast.Ident, typ ast.Expr, pointerMethods map[string]map[string]bool) {
	if id, ok := typ.(*ast.Ident); ok && pointerMethods[id.Name] != nil {
		for _, n := range names {
			copies[n.Name] = id.Name
		}
	}
}
