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

// storageException is a function outside storage.go that assigns value storage itself.
type storageException struct {
	// count is the number of places where the function does so.
	count int

	reason string
}

// objectStorageExceptions are the functions that assign the slots or the elements of objects.
var objectStorageExceptions = map[string]storageException{
	"agent.exec": {
		count:  7,
		reason: "adds within the capacity and creates objects; calls in exec cost more than inlined stores",
	},
	"QuietContext.AppendElements": {
		count:  1,
		reason: "appends within the room that reserveElems made; a helper costs each push a spill",
	},
	"QuietContext.AppendElementsOf": {
		count:  1,
		reason: "calls leaveElems itself; a helper is not inlined",
	},
	"QuietContext.PrependElements": {
		count:  2,
		reason: "calls leaveElems itself; a helper is not inlined",
	},
	"Object.RemoveFirstElement": {
		count:  2,
		reason: "clears the removed element itself; a helper is not inlined",
	},
	"QuietContext.SpliceElements": {
		count:  1,
		reason: "calls leaveElems and clears the tail itself; a helper is not inlined",
	},
	"ExecutionContext.setArrayLength": {
		count:  2,
		reason: "clears the tail itself with polls; a helper is not inlined",
	},
	"ExecutionContext.DefineMatchResultProperties": {
		count:  1,
		reason: "fills the slots of a new array; a helper copies the values twice",
	},
	"ExecutionContext.DefineMatchResultPropertiesWithIndices": {
		count:  1,
		reason: "fills the slots of a new array; a helper copies the values twice",
	},
	"ExecutionContext.NewArrayCopy": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"realmPart.NewArrayPair": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"ExecutionContext.CreateIterResultObject": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"ExecutionContext.BoundFunctionCreate": {
		count:  1,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"ExecutionContext.newArgumentsObject": {
		count:  2,
		reason: "creates the object; a helper adds a write barrier sequence",
	},
	"propertyObject.init": {
		count:  1,
		reason: "creates the object",
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
		reason: "clears the tail itself; truncated costs every completion spills",
	},
	"jobQueue.dequeue": {
		count:  2,
		reason: "clears each job as it leaves; truncated costs every job spills",
	},
	"ExecutionContext.growNatives": {
		count:  1,
		reason: "limits the capacity of new storage",
	},
}

// TestValueStorageOwnership tests that only storage.go changes which storage holds the slots and
// the elements of objects, and that other long-lived buffers of values are shortened only with
// truncated and withoutLast, which clear what they give up.
//
// For slots and elems it covers assignments of the fields and of the structs that contain them,
// pointers to the fields, and appends to them. For the other buffers it covers assignments of a
// slice expression. It leaves out the registers on the stack and in generators, the frames
// (agent.clearStale clears those), composite literals of new objects, local copies of a slice,
// and stores of single values.
func TestValueStorageOwnership(t *testing.T) {
	prog := checkProgram(t, "../../...")
	object := map[types.Object]bool{
		prog.field(t, "objectProperties", "slots"): true,
		prog.field(t, "objectProperties", "elems"): true,
	}
	// Assigning one of these replaces slots and elems at once.
	whole := []*types.Named{
		prog.typ(t, "objectProperties"),
		prog.typ(t, "propertyObject"),
		prog.typ(t, "Object"),
	}
	properties := prog.field(t, "Object", "objectProperties")
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
			if p.isEngine() && filepath.Base(p.fset.Position(file.Pos()).Filename) == "storage.go" {
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
