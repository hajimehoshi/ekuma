// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"testing"
)

// rawStoreCalls are the methods of storage.Raw that store, or that return what can be stored to, with
// the number of calls in agent.exec.
var rawStoreCalls = map[string]int{
	"ExtendElems": 3,
	"ExtendSlots": 2,
	"SetElem":     5,
	"SetFlags":    1,
	"SetLength":   4,
	"SetShape":    8,
	"SetSlot":     3,
	"Elems":       3,
	"SlotRef":     0,
}

// rawLoadMethods are the other methods.
var rawLoadMethods = []string{"Elem", "ElemCount", "ElemRoom", "ElemsShared", "Slot", "SlotCount", "SlotRoom"}

// unguardedUses are the operations of storage that skip the checks of the property operations,
// with the functions outside storage that use them and how often they do.
var unguardedUses = map[string]map[string]int{
	"CacheEntry.AddTo": {
		"ExecutionContext.definePublicField": 1,
	},
	"CacheEntry.OwnValue": {
		"ExecutionContext.GetVCached": 2,
	},
	"GlobalCache.Remember": {
		"ExecutionContext.getGlobal":        1,
		"ExecutionContext.getGlobalLexical": 1,
		"ExecutionContext.setGlobal":        2,
	},
	"Object.AppendElem": {
		"agent.exec": 1,
	},
	"Object.AppendSlot": {
		"agent.exec": 2,
	},
	"Object.FreezeLength": {
		"ExecutionContext.defineArrayLength": 1,
		"agent.exec":                         1,
	},
	"Object.GrowLength": {
		"ExecutionContext.setArrayLength": 1,
	},
	"Object.InitBinding": {
		"ExecutionContext.initGlobalLex": 1,
	},
	"Object.PushElement": {
		"ExecutionContext.appendIterable": 1,
		"agent.exec":                      2,
	},
	"Object.SetArrayIndex": {
		"ExecutionContext.indexString":    1,
		"ExecutionContext.newIndexString": 1,
	},
	"Object.SlotValue": {
		"ExecutionContext.GetOwnProperty":     1,
		"ExecutionContext.defineClassElement": 1,
		"ExecutionContext.getGlobal":          1,
		"ExecutionContext.getGlobalLexical":   1,
		"ExecutionContext.getIndex":           1,
		"ExecutionContext.getPrivate":         1,
		"ExecutionContext.setGlobal":          1,
		"ExecutionContext.setNamed":           1,
		"ExecutionContext.setPrivate":         1,
	},
	"Object.StoreElement": {
		"ExecutionContext.DeleteProp": 1,
		"ExecutionContext.setIndex":   1,
		"ExecutionContext.storeValue": 1,
	},
	"Object.StoreMapped": {
		"ExecutionContext.defineMappedArgument": 3,
	},
	"Poller.TruncateElements": {
		"ExecutionContext.setArrayLength": 1,
		"ExecutionContext.truncateArray":  1,
	},
	"PropCache.RememberAbsent": {
		"ExecutionContext.getNamed": 1,
	},
	"RealmPart.AddMappedElement": {
		"ExecutionContext.newArguments": 1,
	},
	"Space.Remove": {
		"ExecutionContext.deleteMappedArgument": 1,
		"ExecutionContext.deleteOwnSlot":        1,
		"ExecutionContext.setArrayLength":       1,
	},
	"Object.InitElems": {
		"agent.exec": 1,
	},
	"Object.InitSlots": {
		"agent.exec": 1,
	},
}

// TestUnguardedOperations tests that the functions outside storage that use the operations in
// unguardedUses are those listed there. It does not see a function value made from an operation
// in storage.
func TestUnguardedOperations(t *testing.T) {
	prog := checkProgram(t, "../../...")
	inStorage := func(_ *types.Info, file *ast.File) map[*ast.Ident]bool {
		ids := map[*ast.Ident]bool{}
		if file.Name.Name != "storage" {
			return ids
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				ids[id] = true
			}
			return true
		})
		return ids
	}
	for _, name := range slices.Sorted(maps.Keys(unguardedUses)) {
		prog.checkUseCounts(t, "use of "+name, prog.function(t, name), useCounts{
			"": unguardedUses[name],
		}, inStorage)
	}
}

// TestRawAccess tests that only agent.exec uses storage.Raw, apart from the files that check the
// others in the ekumavalidate build, and how often it stores through it.
func TestRawAccess(t *testing.T) {
	prog := checkProgram(t, "../../...")
	prog.checkUses(t, useRule{
		what:         "raw access to an object",
		objs:         prog.function(t, "Object.Raw"),
		owners:       []string{"agent.exec"},
		skipValidate: true,
	})
	methods := map[string]bool{}
	for m := range prog.typ(t, "Raw").Methods() {
		methods[m.Name()] = true
	}
	for name, n := range rawStoreCalls {
		want := useCounts{}
		if n > 0 {
			want["adds within the capacity and creates objects; calls in exec cost more than inlined stores"] = map[string]int{"agent.exec": n}
		}
		prog.checkUseCounts(t, "call of Raw."+name, prog.function(t, "Raw."+name), want, nil)
		delete(methods, name)
	}
	for _, name := range rawLoadMethods {
		delete(methods, name)
	}
	for _, name := range slices.Sorted(maps.Keys(methods)) {
		t.Errorf("Raw.%s is in neither list", name)
	}
}
