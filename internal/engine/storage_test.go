// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"testing"
)

// storageException is a function outside valuestorage.go that assigns value storage itself.
type storageException struct {
	// count is the number of places where the function does so.
	count int

	reason string
}

// objectStorageExceptions are the functions that assign the slots or the elements of objects.
var objectStorageExceptions = map[string]storageException{
	"realmPart.appendElements": {
		count:  1,
		reason: "appends within the room that reserveElems made; a helper costs each push a spill",
	},
	"realmPart.appendElementsOf": {
		count:  1,
		reason: "calls leaveElems itself; a helper is not inlined",
	},
	"realmPart.prependElements": {
		count:  2,
		reason: "calls leaveElems itself; a helper is not inlined",
	},
	"Object.RemoveFirstElement": {
		count:  1,
		reason: "clears the removed element itself; a helper is not inlined",
	},
	"realmPart.spliceElements": {
		count:  1,
		reason: "calls leaveElems and clears the tail itself; a helper is not inlined",
	},
	"realmPart.DefineMatchResultProperties": {
		count:  1,
		reason: "fills the slots of a new array; a helper copies the values twice",
	},
	"realmPart.DefineMatchResultPropertiesWithIndices": {
		count:  1,
		reason: "fills the slots of a new array; a helper copies the values twice",
	},
	"realmPart.NewArrayPair": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"realmPart.CreateIterResultObject": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"propertyObject.initMaterializedFunction": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"realmPart.newArgumentsObject": {
		count:  2,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"Poller.truncateElements": {
		count:  1,
		reason: "clears the tail itself with polls; a helper is not inlined",
	},
	"realmPart.NewArrayCopy": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"Raw.ExtendElems": {
		count:  1,
		reason: "is what the interpreter loop uses",
	},
	"Raw.ExtendSlots": {
		count:  1,
		reason: "is what the interpreter loop uses",
	},
	"propertyObject.init": {
		count:  1,
		reason: "creates the object",
	},
	"plainObject.init": {
		count:  1,
		reason: "creates the object",
	},
	"Object.newTail": {
		count:  1,
		reason: "gives the object a tail of its own",
	},
	"Object.ensureWeak": {
		count:  1,
		reason: "gives the object a tail of its own with inline weak entries",
	},
	"Object.text": {
		count:  1,
		reason: "the text of a string or a symbol, which has no slots, is in their words",
	},
}

// bufferExceptions are the functions that reslice the other long-lived buffers.
var bufferExceptions = map[string]storageException{
	"ExecutionContext.pushNative": {
		count:  1,
		reason: "grows within the capacity",
	},
	"ExecutionContext.invokeNative": {
		count:  1,
		reason: "grows within the capacity",
	},
	"agent.popNative": {
		count:  1,
		reason: "clears the entry itself on the path of every native call",
	},
	"agent.dropNatives": {
		count:  1,
		reason: "clears each entry in its loop",
	},
	"agent.closeUpvals": {
		count:  1,
		reason: "clears each entry in its loop",
	},
	"generator.closeUpvals": {
		count:  1,
		reason: "clears the tail itself; storage.Truncated costs every completion spills",
	},
	"jobQueue.dequeue": {
		count:  2,
		reason: "clears each job as it leaves; storage.Truncated costs every job spills",
	},
	"ExecutionContext.growNatives": {
		count:  1,
		reason: "limits the capacity of new storage",
	},
}

// TestValueStorageOwnership tests that only valuestorage.go changes which storage holds the slots and
// the elements of objects, and that other long-lived buffers of values are shortened only with
// [storage.Truncated] and [storage.WithoutLast], which clear what they give up.
//
// For slots and elems it covers assignments of the fields and of the structs that contain them,
// pointers to the fields, and appends to them. For the other buffers it covers assignments of a
// slice expression. It leaves out the registers on the stack and in generators, the frames
// (agent.clearStale clears those), composite literals of new objects, local copies of a slice,
// and stores of single values.
func TestValueStorageOwnership(t *testing.T) {
	prog := checkProgram(t, "../../...")
	object := map[types.Object]bool{
		prog.field(t, "Object", "slots"):     true,
		prog.field(t, "objectTail", "elems"): true,
	}
	// Assigning one of these replaces slots or elems at once.
	whole := []*types.Named{
		prog.typ(t, "objectTail"),
		prog.typ(t, "propertyObject"),
		prog.typ(t, "plainObject"),
		prog.typ(t, "Object"),
	}
	properties := prog.field(t, "Object", "objectTail")
	buffers := map[types.Object]bool{
		prog.field(t, "agent", "natives"):             true,
		prog.field(t, "agent", "openUpvals"):          true,
		prog.field(t, "generator", "openUpvals"):      true,
		prog.field(t, "generator", "scopes"):          true,
		prog.field(t, "frame", "scopes"):              true,
		prog.field(t, "asyncState", "queue"):          true,
		prog.field(t, "DisposeCapability", "stack"):   true,
		prog.field(t, "rejectionTracker", "promises"): true,
		prog.field(t, "jobQueue", "jobs"):             true,
	}
	for _, exceptions := range []map[string]storageException{objectStorageExceptions, bufferExceptions} {
		for name := range exceptions {
			prog.checkUnique(t, name)
		}
	}

	// base returns the expression that e is a slice of.
	base := func(e ast.Expr) ast.Expr {
		for {
			s, ok := ast.Unparen(e).(*ast.SliceExpr)
			if !ok {
				return e
			}
			e = s.X
		}
	}
	// isWhole reports whether typ is one of whole, and not a pointer to it.
	isWhole := func(typ types.Type) bool {
		_, ok := types.Unalias(typ).(*types.Named)
		return ok && slices.ContainsFunc(whole, func(w *types.Named) bool {
			return isType(typ, w)
		})
	}

	objectCounts, bufferCounts := map[string]int{}, map[string]int{}
	for _, p := range prog.pkgs {
		fieldOf := func(e ast.Expr) types.Object {
			if sel, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
				return used(p.info.Uses[sel.Sel])
			}
			return nil
		}
		isAppend := func(call *ast.CallExpr) bool {
			id, ok := ast.Unparen(call.Fun).(*ast.Ident)
			if !ok {
				return false
			}
			b, ok := p.info.Uses[id].(*types.Builtin)
			return ok && b.Name() == "append"
		}

		for _, file := range p.files {
			if p.path == storagePath && filepath.Base(p.fset.Position(file.Pos()).Filename) == "valuestorage.go" {
				continue
			}
			for _, decl := range file.Decls {
				report := func(pos token.Pos, exceptions map[string]storageException, counts map[string]int, msg string) {
					owner := prog.owner(&p, decl, slices.Collect(maps.Keys(exceptions)))
					counts[owner]++
					if counts[owner] > exceptions[owner].count {
						t.Errorf("%v: %s in %s", p.fset.Position(pos), msg, owner)
					}
				}
				// assigned holds the appends whose result an assignment reported already stores.
				assigned := map[ast.Expr]bool{}
				ast.Inspect(decl, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.AssignStmt:
						if n.Tok == token.DEFINE {
							return true
						}
						for i, l := range n.Lhs {
							f := fieldOf(l)
							star, _ := ast.Unparen(l).(*ast.StarExpr)
							switch {
							case object[f], f != nil && f == properties, star != nil && isWhole(p.info.TypeOf(star)):
								report(l.Pos(), objectStorageExceptions, objectCounts, "assignment of object storage")
								if len(n.Lhs) == len(n.Rhs) {
									assigned[ast.Unparen(n.Rhs[i])] = true
								}
							case buffers[f] && len(n.Lhs) == len(n.Rhs):
								if _, ok := ast.Unparen(n.Rhs[i]).(*ast.SliceExpr); ok {
									report(l.Pos(), bufferExceptions, bufferCounts, "reslice of a buffer")
								}
							}
						}
					case *ast.RangeStmt:
						for _, e := range []ast.Expr{n.Key, n.Value} {
							if e != nil && object[fieldOf(e)] {
								report(e.Pos(), objectStorageExceptions, objectCounts, "assignment of object storage")
							}
						}
					case *ast.UnaryExpr:
						if n.Op == token.AND && object[fieldOf(n.X)] {
							report(n.Pos(), objectStorageExceptions, objectCounts, "pointer to object storage")
						}
					case *ast.CallExpr:
						if isAppend(n) && !assigned[n] && object[fieldOf(base(n.Args[0]))] {
							report(n.Pos(), objectStorageExceptions, objectCounts, "append to object storage")
						}
					}
					return true
				})
			}
		}
	}
	for _, tc := range []struct {
		exceptions map[string]storageException
		counts     map[string]int
	}{
		{
			exceptions: objectStorageExceptions,
			counts:     objectCounts,
		},
		{
			exceptions: bufferExceptions,
			counts:     bufferCounts,
		},
	} {
		for name, e := range tc.exceptions {
			if got := tc.counts[name]; got < e.count {
				t.Errorf("%s: got %d places, want %d (%s)", name, got, e.count, e.reason)
			}
		}
	}
}

// sharedTailExceptions are the functions that store the function state or the internal state of
// an object through its tail pointer, which must not be the shared empty tail there.
var sharedTailExceptions = map[string]storageException{
	"Object.OwnFunction": {
		count:  1,
		reason: "a function object has a tail of its own",
	},
	"NewRegExpObject": {
		count:  1,
		reason: "creates the object with a tail of its own",
	},
}

// TestSharedTailWrites tests that the function state and the internal state of objects are stored
// through the tail pointer of an object only where it cannot be the shared empty tail of the
// objects that need neither: through Object.ownTail, through the tail of an allocation that has one
// of its own, or in sharedTailExceptions. The assignments of elems are in TestValueStorageOwnership.
func TestSharedTailWrites(t *testing.T) {
	prog := checkProgram(t, "../../...")
	tail := prog.field(t, "Object", "objectTail")
	watched := map[types.Object]bool{
		prog.field(t, "objectTail", "Fn"):  true,
		prog.field(t, "objectTail", "Ext"): true,
	}
	for name := range sharedTailExceptions {
		prog.checkUnique(t, name)
	}

	counts := map[string]int{}
	for _, p := range prog.pkgs {
		// throughTail reports whether e is a watched field reached through the tail pointer.
		throughTail := func(e ast.Expr) bool {
			sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
			if !ok || !watched[used(p.info.Uses[sel.Sel])] {
				return false
			}
			typ := p.info.TypeOf(sel.X)
			_, index, _ := types.LookupFieldOrMethod(typ, true, p.info.Uses[sel.Sel].Pkg(), sel.Sel.Name)
			for _, i := range index[:max(len(index)-1, 0)] {
				if ptr, ok := typ.Underlying().(*types.Pointer); ok {
					typ = ptr.Elem()
				}
				st, ok := typ.Underlying().(*types.Struct)
				if !ok {
					return false
				}
				f := st.Field(i)
				if f.Origin() == tail {
					return true
				}
				typ = f.Type()
			}
			return false
		}
		for _, file := range p.files {
			for _, decl := range file.Decls {
				report := func(pos token.Pos, msg string) {
					owner := prog.owner(&p, decl, slices.Collect(maps.Keys(sharedTailExceptions)))
					counts[owner]++
					if counts[owner] > sharedTailExceptions[owner].count {
						t.Errorf("%v: %s in %s", p.fset.Position(pos), msg, owner)
					}
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.AssignStmt:
						if n.Tok == token.DEFINE {
							return true
						}
						for _, l := range n.Lhs {
							if throughTail(l) {
								report(l.Pos(), "store through a tail pointer")
							}
						}
					case *ast.UnaryExpr:
						if n.Op == token.AND && throughTail(n.X) {
							report(n.Pos(), "pointer through a tail pointer")
						}
					}
					return true
				})
			}
		}
	}
	for name, e := range sharedTailExceptions {
		if got := counts[name]; got < e.count {
			t.Errorf("%s: got %d places, want %d (%s)", name, got, e.count, e.reason)
		}
	}
}
