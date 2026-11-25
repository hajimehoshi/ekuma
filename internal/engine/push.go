// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

const pushSampleInterval = 8

// pushSizeHint tracks single-element pushes at one call site in a JavaScript closure.
type pushSizeHint struct {
	pc int
	// previous and current are the sampled sizes of successive runs at pc.
	// A push reaching minElems+1 starts a new run.
	previous uint16
	current  uint16
	// target is the smaller of the two completed samples when a run starts.
	target uint16
}

// PushElements appends vals to the array o and returns its new length and true.
// If o requires the generic Array.prototype.push algorithm, it leaves o unchanged
// and returns 0, false.
func (q QuietContext) PushElements(o *Object, vals []Value) (uint32, bool) {
	n, ok := q.AppendElements(o, vals)
	// Sample single-element pushes so earlier array lengths at this call site can
	// guide how much space to reserve.
	if ok && len(vals) == 1 && n > minElems && n <= maxPresizedElems {
		if n == minElems+1 || int(n) == cap(o.elems) || n%pushSampleInterval == 0 {
			q.observePush(o)
		}
	}
	return n, ok
}

// observePush is [agent.observePush].
func (q QuietContext) observePush(o *Object) {
	q.realm.executionContext().observePush(o)
}

// observePush samples the array after a single-element push at the calling frame's pc.
func (ag *agent) observePush(o *Object) {
	if len(ag.frames) == 0 {
		return
	}
	n := len(o.elems)
	fr := &ag.frames[len(ag.frames)-1]
	state := fr.cl.extState()
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
			fr.cl.setExtState(h)
		}
		*h = pushSizeHint{
			pc: fr.pc,
		}
	}
	if n == minElems+1 {
		h.target, h.previous, h.current = min(h.previous, h.current), h.current, 0
	}
	if n >= 2*pushSampleInterval {
		// The final length can fall between samples. Limit the allowance
		// to space the array already acquired.
		h.current = uint16(min(n+pushSampleInterval-1, cap(o.elems), maxPresizedElems))
	}
	if n == cap(o.elems) {
		// Bound the cost of a short result following larger ones.
		if room := min(int(h.target), 8*n); room > n {
			o.moveElemsTo(room)
		}
	}
}
