// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

const pushSampleInterval = 8

// pushSizeHint tracks single-element pushes at one call site in a JavaScript closure.
type pushSizeHint struct {
	pc int
	// previous and current are the sampled sizes of successive runs at pc.
	// A push reaching storage.MinElems+1 starts a new run.
	previous uint16
	current  uint16
	// target is the smaller of the two completed samples when a run starts.
	target uint16
}

// PushElements appends vals to the array o and returns its new length and true.
// If o requires the generic Array.prototype.push algorithm, it leaves o unchanged
// and returns 0, false.
func PushElements(q quiet.Context, o *storage.Object, vals []storage.Value) (uint32, bool) {
	if len(vals) == 1 {
		return pushElement(q, o, vals[0])
	}
	return q.AppendElements(o, vals)
}

// ArrayPushKernel is the shortcut of Array.prototype.push for one argument.
func ArrayPushKernel(q quiet.Context, this, v storage.Value) (storage.Value, bool) {
	if !this.IsObject() {
		return storage.Undefined, false
	}
	n, ok := pushElement(q, this.O, v)
	if !ok {
		return storage.Undefined, false
	}
	return storage.Number(float64(n)), true
}

func pushElement(q quiet.Context, o *storage.Object, v storage.Value) (uint32, bool) {
	n, ok := q.AppendElement(o, v)
	// Earlier array lengths at this call site guide how much space to reserve.
	if ok && n > storage.MinElems && n <= storage.MaxPresizedElems && pushSample(n, o.ElemRoom()) {
		observePush(q, o)
	}
	return n, ok
}

// pushSample tests lengths in the sampling range (MinElems, MaxPresizedElems].
func pushSample(n uint32, room int) bool {
	return n == storage.MinElems+1 || int(n) == room || n%pushSampleInterval == 0
}

// observePush is [agent.observePush].
func observePush(q quiet.Context, o *storage.Object) {
	executionContextOf(q.Realm).observePush(o)
}

// observePush samples the array after a single-element push at the calling frame's pc.
func (ag *agent) observePush(o *storage.Object) {
	if len(ag.frames) == 0 {
		return
	}
	n := o.ElemCount()
	fr := &ag.frames[len(ag.frames)-1]
	state := fr.cl.ExtState()
	h, _ := state.(*pushSizeHint)
	if h == nil || h.pc != fr.pc {
		if n < 2*pushSampleInterval {
			return
		}
		if h == nil {
			if state != nil {
				return
			}
			h = new(pushSizeHint)
			fr.cl.SetExtState(h)
		}
		*h = pushSizeHint{
			pc: fr.pc,
		}
	}
	if n == storage.MinElems+1 {
		h.target, h.previous, h.current = min(h.previous, h.current), h.current, 0
	}
	if n >= 2*pushSampleInterval {
		// The final length can fall between samples. Limit the allowance
		// to space the array already acquired.
		h.current = uint16(min(n+pushSampleInterval-1, o.ElemRoom(), storage.MaxPresizedElems))
	}
	if n == o.ElemRoom() {
		// Bound the cost of a short result following larger ones.
		if room := min(int(h.target), 8*n); room > n {
			o.MoveElemsTo(room)
		}
	}
}
