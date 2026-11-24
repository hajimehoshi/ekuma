// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package waiterlist provides lists of waiters that refer to the waiters weakly.
package waiterlist

import (
	"weak"
)

// Waiter is a waiter with the data of its user, which is in at most one list.
type Waiter[T any] struct {
	// Data is the data of the user.
	Data T

	// index is the index of the waiter list that the waiter is in or was last in.
	index int

	// listed reports whether the waiter is in its waiter list, and pos is its position there.
	listed bool
	pos    int
}

// Lists holds waiter lists by index, which do not keep their waiters from being collected, and must
// not be used concurrently.
type Lists[T any] struct {
	// lists holds only the lists in which waiters are listed, collected ones included.
	lists map[int]*list[T]
}

// Add adds w, which must not be in any list, at the end of the list of index.
func (s *Lists[T]) Add(index int, w *Waiter[T]) {
	if s.lists == nil {
		s.lists = map[int]*list[T]{}
	}
	l := s.lists[index]
	if l == nil {
		l = &list[T]{}
		s.lists[index] = l
	}
	if len(l.entries) >= 2*max(l.swept, minSweep) {
		l.sweep()
	}
	w.index = index
	w.listed = true
	w.pos = l.base + len(l.entries)
	l.entries = append(l.entries, weak.Make(w))
	l.listed++
}

// Remove removes w, which must not be in the lists of another Lists, from its list and reports
// whether it was in one.
func (s *Lists[T]) Remove(w *Waiter[T]) bool {
	if !w.listed {
		return false
	}
	l := s.lists[w.index]
	l.remove(w)
	if l.listed == 0 {
		delete(s.lists, w.index)
	}
	return true
}

// RemoveFirst removes and returns the first count waiters of the list of index. count must not be
// negative and may be +Inf.
func (s *Lists[T]) RemoveFirst(index int, count float64) []*Waiter[T] {
	if s.lists[index] == nil {
		return nil
	}
	return s.removeFirst(index, count)
}

// removeFirst implements RemoveFirst for an index that has a list. The collected waiters do not
// count.
func (s *Lists[T]) removeFirst(index int, count float64) []*Waiter[T] {
	l := s.lists[index]
	var ws []*Waiter[T]
	n := 0
	for ; n < len(l.entries) && float64(len(ws)) < count; n++ {
		e := l.entries[n]
		if e == (weak.Pointer[Waiter[T]]{}) {
			continue
		}
		l.listed--
		if w := e.Value(); w != nil {
			w.listed = false
			ws = append(ws, w)
		}
	}
	clear(l.entries[:n])
	l.entries = l.entries[n:]
	l.base += n
	if l.listed == 0 {
		delete(s.lists, index)
	}
	return ws
}

// minSweep is the length of a waiter list from which adding to it may sweep it.
const minSweep = 16

// list is a waiter list, in the order in which the waiters were added. A removed or
// collected waiter leaves an empty entry until a sweep, which runs when the list has doubled
// since the last one.
type list[T any] struct {
	// entries holds the waiters from the position base on.
	entries []weak.Pointer[Waiter[T]]
	base    int

	// listed is the number of the waiters in entries that are listed, collected ones included.
	listed int

	// swept is the length of entries after the last sweep.
	swept int
}

// remove removes the listed waiter w from l.
func (l *list[T]) remove(w *Waiter[T]) {
	w.listed = false
	l.entries[w.pos-l.base] = weak.Pointer[Waiter[T]]{}
	l.listed--
}

// sweep removes the empty entries and those of the collected waiters from l.
func (l *list[T]) sweep() {
	n := 0
	for _, e := range l.entries {
		if e == (weak.Pointer[Waiter[T]]{}) {
			continue
		}
		w := e.Value()
		if w == nil {
			l.listed--
			continue
		}
		w.pos = l.base + n
		l.entries[n] = e
		n++
	}
	clear(l.entries[n:])
	l.entries = l.entries[:n]
	l.swept = n
}
