// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// Script is a script parsed by [Realm.ParseScript].
type Script struct {
	vm *VM
	s  *engine.Script
}

// ParseScript is [Realm.ParseScript] of the initial realm.
func (vm *VM) ParseScript(name, src string) (*Script, error) {
	vm.initIfNeeded()
	return vm.initialRealm.ParseScript(name, src)
}

// Evaluate evaluates s, runs the pending jobs, and returns the completion value of s. s may be
// evaluated more than once, but evaluating it again throws a JavaScript SyntaxError when s
// declares a top-level let, const or class.
//
// The error is a [*ScriptError] when the script throws. When ctx is done, the script stops at its
// next loop iteration or call, and the error is the cause of ctx.
// Internal operation failures return an [*InternalError].
func (s *Script) Evaluate(ctx context.Context) (Value, error) {
	vm := s.vm
	vm.initIfNeeded()
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.e.ScriptEvaluation(s.s)
	})
}
