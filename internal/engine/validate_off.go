// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !ekumavalidate

package engine

// validateExec says whether the interpreter checks the code that it runs, as validate_on.go
// describes.
const validateExec = false

type execChecks struct{}

func (ag *agent) enterChecks() {}

func (ag *agent) leaveChecks() {}

func (ag *agent) checkStep(fp *funcProto, pc int, regs []Value) {}
