// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// ExecutionContext binds engine operations to one realm of an agent.
type ExecutionContext struct {
	*agent
	realm *realm
}

// Host provides agent-wide operations and the active context for host calls.
// A Host must not be copied.
type Host struct {
	agent
}

// ActiveContext returns the execution context for a host entry.
func (h *Host) ActiveContext() *ExecutionContext {
	return h.active
}

// activate installs cx and returns the context it replaced.
func (cx *ExecutionContext) activate() *ExecutionContext {
	previous := cx.active
	if previous != cx {
		cx.active = cx
	}
	return previous
}

// Run calls f with cx active for host reentry.
func (cx *ExecutionContext) Run(f func()) {
	if previous := cx.activate(); previous != cx {
		defer previous.activate()
	}
	f()
}
