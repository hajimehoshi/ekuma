// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package engine

import "sync/atomic"

// interruptFlag is the flag of a pending interruption.
type interruptFlag = atomic.Bool

// pollTick accounts for a call of Poll.
func (vm *VM) pollTick() {}
