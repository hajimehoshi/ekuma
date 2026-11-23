// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package engine

import "sync/atomic"

// interruptFlag is the flag of a pending interruption.
type interruptFlag = atomic.Bool

// pollYield does nothing, as the other goroutines run without waiting for a yield.
func (ag *agent) pollYield() {}
