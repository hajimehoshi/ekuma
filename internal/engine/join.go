// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "slices"

// joinGuard holds the objects that are being joined, to stop at cycles.
type joinGuard struct {
	joining []*Object
}

// enter adds o and reports whether it was not being joined. leave must be called when it
// reports true.
func (g *joinGuard) enter(o *Object) bool {
	if slices.Contains(g.joining, o) {
		return false
	}
	g.joining = append(g.joining, o)
	return true
}

// leave removes the object that the last call of enter added.
func (g *joinGuard) leave() {
	last := len(g.joining) - 1
	g.joining[last] = nil
	g.joining = g.joining[:last]
}

// EnterJoin starts Array.prototype.join of the current realm for o and reports whether it was
// not running for o. [ExecutionContext.LeaveJoin] must be called when it reports true.
func (cx *ExecutionContext) EnterJoin(o *Object) bool {
	return cx.realm.joins.enter(o)
}

// LeaveJoin ends what the last successful call of [ExecutionContext.EnterJoin] started.
func (cx *ExecutionContext) LeaveJoin() {
	cx.realm.joins.leave()
}

// EnterLocaleJoin is [ExecutionContext.EnterJoin] for Array.prototype.toLocaleString.
func (cx *ExecutionContext) EnterLocaleJoin(o *Object) bool {
	return cx.realm.localeJoins.enter(o)
}

// LeaveLocaleJoin is [ExecutionContext.LeaveJoin] for Array.prototype.toLocaleString.
func (cx *ExecutionContext) LeaveLocaleJoin() {
	cx.realm.localeJoins.leave()
}
