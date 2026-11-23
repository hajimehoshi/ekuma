// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// The unexported embedded name keeps the raw VM outside the context API.
type agent = VM

// ExecutionContext binds engine operations to one realm of an agent.
type ExecutionContext struct {
	*agent
	realm *Realm
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

// Context returns the execution context of r.
func (r *Realm) Context() *ExecutionContext {
	return &r.context
}

// Realm returns the execution realm.
func (cx *ExecutionContext) Realm() *Realm {
	return cx.realm
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

// CreateIterResultObject creates an iterator result in the execution realm.
func (cx *ExecutionContext) CreateIterResultObject(v Value, done bool) Value {
	return cx.realm.CreateIterResultObject(v, done)
}
