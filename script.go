// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// Script is a script of a realm, from [Realm.ParseScript] or [Realm.BindScript].
type Script struct {
	vm *VM
	s  *engine.Script
}

// UnboundScript is a script parsed for no particular VM, which [Realm.BindScript] binds to a
// realm. An UnboundScript is immutable: any number of VMs and realms may bind it, also from
// different goroutines at the same time.
type UnboundScript struct {
	s *engine.UnboundScript
}

// ParseUnboundScript parses src as the source text of a script, as [Realm.ParseScript] does,
// but for no particular VM. The error is a [*SyntaxError] when src is not a valid script.
func ParseUnboundScript(name, src string) (*UnboundScript, error) {
	s, err := engine.ParseUnboundScript(name, src)
	if err != nil {
		return nil, publicError(err)
	}
	return &UnboundScript{
		s: s,
	}, nil
}

// ParseScript is [Realm.ParseScript] of the initial realm.
func (vm *VM) ParseScript(name, src string) (*Script, error) {
	vm.initIfNeeded()
	return vm.initialRealm.ParseScript(name, src)
}

// BindScript is [Realm.BindScript] of the initial realm.
func (vm *VM) BindScript(u *UnboundScript) *Script {
	vm.initIfNeeded()
	return vm.initialRealm.BindScript(u)
}

// Evaluate evaluates s and returns its completion value. A script may be evaluated more than
// once, but evaluating it again throws a JavaScript SyntaxError when s declares a top-level
// let, const or class.
//
// Pending promise jobs run before the outermost Go call that executes JavaScript returns.
// An Evaluate call made from a host callback can return with jobs still pending.
// An interruption discards pending jobs.
//
// The error is a [*ScriptError] when the script throws. When ctx is done, the script stops at its
// next loop iteration or call, and the error is the cause of ctx.
// Internal operation failures return an [*InternalError].
func (s *Script) Evaluate(ctx context.Context) (Value, error) {
	v, _, err := s.evaluate(ctx)
	return v, err
}

func (s *Script) evaluate(ctx context.Context) (Value, bool, error) {
	vm := s.vm
	vm.initIfNeeded()
	var completed bool
	v, err := vm.run(ctx, func() (engine.Value, error) {
		value, done, err := vm.e.ScriptEvaluation(s.s)
		completed = done
		return value, err
	})
	return v, completed, err
}
