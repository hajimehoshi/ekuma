// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !ekuma_validate

package engine

// validateExec says whether the interpreter checks the code that it runs, as validate_on.go
// describes.
const validateExec = false

type execChecks struct{}

func (vm *VM) enterChecks() {}

func (vm *VM) leaveChecks() {}

func (vm *VM) checkStep(fp *funcProto, pc int, regs []Value) {}
