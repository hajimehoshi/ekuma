// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package storage

import (
	"sync/atomic"
)

// interruptFlag is the flag of a pending interruption.
type interruptFlag = atomic.Bool

// PollYield does nothing, as the other goroutines run without waiting for a yield.
func (p *Poller) PollYield() {}
