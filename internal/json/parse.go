// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// maxRetainedBuffer is the largest string buffer that a [Parser] keeps for the next call.
const maxRetainedBuffer = 1 << 23

// Record is a JSON Parse Record: what a part of the text produced, for [Revive].
type Record struct {
	value engine.Value

	// source is the text of a primitive value.
	source wtf8.String

	// elems are the records of the elements of an array.
	elems []*Record

	// keys and entries are the records of the properties of an object, by key.
	// A repeated key has the record of its last occurrence.
	keys    []engine.PropKey
	entries []*Record

	// index maps a key to its position in keys, for an object with many keys.
	index map[engine.PropKey]int
}

// element returns the record of the element i of an array, or nil when there is none.
func (r *Record) element(i int64) *Record {
	if i < int64(len(r.elems)) {
		return r.elems[i]
	}
	return nil
}

// entry returns the record of the property k of an object, or nil when there is none.
func (r *Record) entry(k engine.PropKey) *Record {
	if len(r.keys) > 16 {
		if r.index == nil {
			r.index = make(map[engine.PropKey]int, len(r.keys))
			for i, k := range r.keys {
				r.index[k] = i
			}
		}
		if i, ok := r.index[k]; ok {
			return r.entries[i]
		}
		return nil
	}
	for i, v := range slices.Backward(r.keys) {
		if v == k {
			return r.entries[i]
		}
	}
	return nil
}

// frame is an array or object whose members are being parsed.
type frame struct {
	array bool

	// base is the index in vals of the first member, and keyBase the index in keys of its key.
	base, keyBase int
}

// Parser parses JSON text into JavaScript values. The zero value is ready to use.
// A Parser keeps scratch storage between calls but no references to values.
type Parser struct {
	cx  *engine.ExecutionContext
	src wtf8.String
	pos int

	frames []frame
	vals   []engine.Value
	keys   []engine.PropKey

	// recs runs parallel to vals when the parse records are wanted.
	recs []*Record

	buf   wtf8.Buffer
	ticks int
}

// Parse parses the JSON text src into a value of cx. It throws a SyntaxError when src is not a JSON text.
func (p *Parser) Parse(cx *engine.ExecutionContext, src wtf8.String) engine.Value {
	v, _ := p.parse(cx, src, false)
	return v
}

// ParseWithRecord is like [Parser.Parse], and also returns the parse record of the value.
func (p *Parser) ParseWithRecord(cx *engine.ExecutionContext, src wtf8.String) (engine.Value, *Record) {
	return p.parse(cx, src, true)
}

func (p *Parser) parse(cx *engine.ExecutionContext, src wtf8.String, withRecords bool) (engine.Value, *Record) {
	p.cx = cx
	p.src = src
	p.pos = 0
	defer p.release()
	return p.parseText(withRecords)
}

// release drops the references that the scratch storage holds, and the storage itself when it grew large.
func (p *Parser) release() {
	p.cx = nil
	p.src = ""
	p.vals = releaseScratch(p.vals)
	p.keys = releaseScratch(p.keys)
	if p.recs != nil {
		p.recs = releaseScratch(p.recs)
	}
	if cap(p.frames) > maxRetainedScratch {
		p.frames = nil
	}
	p.frames = p.frames[:0]
	if p.buf.Len() > maxRetainedBuffer {
		p.buf = wtf8.Buffer{}
	}
	p.buf.Reset()
}

// maxRetainedScratch is the largest number of elements that a scratch slice of the parser keeps for the next call.
const maxRetainedScratch = 1 << 16

func releaseScratch[S ~[]E, E any](s S) S {
	if cap(s) > maxRetainedScratch {
		return nil
	}
	clear(s[:cap(s)])
	return s[:0]
}

func (p *Parser) fail() {
	cx := p.cx
	pos := wtf8.Len(p.src[:p.pos])
	if p.pos >= len(p.src) {
		cx.ThrowSyntaxError("unexpected end of JSON input")
	}
	c := p.src[p.pos]
	if c >= 0x20 && c < 0x7f {
		cx.ThrowSyntaxError("unexpected character '" + string(rune(c)) + "' in JSON at position " + strconv.Itoa(pos))
	}
	cx.ThrowSyntaxError("unexpected character in JSON at position " + strconv.Itoa(pos))
}

func (p *Parser) skipSpace() {
	p.pos = SkipSpace(p.src, p.pos)
}

// parseString parses a string, whose opening quote is at the current position, and returns its contents.
func (p *Parser) parseString() wtf8.String {
	str, end, ok := ScanString(p.src, p.pos, &p.buf)
	p.pos = end
	if !ok {
		p.fail()
	}
	return str
}

func (p *Parser) literal(word wtf8.String) {
	end, ok := ScanLiteral(p.src, p.pos, word)
	p.pos = end
	if !ok {
		p.fail()
	}
}

// parseKey parses the key of an object member and the colon after it.
func (p *Parser) parseKey() {
	if p.pos >= len(p.src) || p.src[p.pos] != '"' {
		p.fail()
	}
	p.keys = append(p.keys, p.cx.KeyOfString(p.parseString()))
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != ':' {
		p.fail()
	}
	p.pos++
}

func (p *Parser) parseText(withRecords bool) (engine.Value, *Record) {
	cx := p.cx
	src := p.src
	var v engine.Value
	var rec *Record
	for {
		// A value starts here.
		p.skipSpace()
		if p.pos >= len(src) {
			p.fail()
		}
		start := p.pos
		switch c := src[p.pos]; c {
		case '{', '[':
			p.pos++
			p.skipSpace()
			array := c == '['
			if p.pos < len(src) && (array && src[p.pos] == ']' || !array && src[p.pos] == '}') {
				p.pos++
				if array {
					v = storage.ObjectValue(cx.NewArray(nil))
				} else {
					v = storage.ObjectValue(cx.NewOrdinaryObject())
				}
				if withRecords {
					rec = &Record{value: v}
				}
				break
			}
			p.frames = append(p.frames, frame{
				array:   array,
				base:    len(p.vals),
				keyBase: len(p.keys),
			})
			if !array {
				p.skipSpace()
				p.parseKey()
			}
			continue
		case '"':
			v = cx.NewString(p.parseString())
		case 't':
			p.literal("true")
			v = storage.True
		case 'f':
			p.literal("false")
			v = storage.False
		case 'n':
			p.literal("null")
			v = storage.Null
		default:
			n, end, ok := ScanNumber(src, p.pos, cx.Poll)
			p.pos = end
			if !ok {
				p.fail()
			}
			v = storage.Number(n)
		}
		if withRecords && rec == nil {
			rec = &Record{
				value:  v,
				source: src[start:p.pos],
			}
		}

		// v is complete. Add it to the enclosing arrays and objects that it completes.
		for {
			if len(p.frames) == 0 {
				p.skipSpace()
				if p.pos != len(src) {
					p.fail()
				}
				return v, rec
			}
			p.vals = append(p.vals, v)
			p.ticks++
			if p.ticks&0x3ff == 0 {
				cx.Poll()
			}
			if withRecords {
				p.recs = append(p.recs, rec)
				rec = nil
			}
			p.skipSpace()
			if p.pos >= len(src) {
				p.fail()
			}
			f := &p.frames[len(p.frames)-1]
			c := src[p.pos]
			p.pos++
			if c == ',' {
				if !f.array {
					p.skipSpace()
					p.parseKey()
				}
				break
			}
			if f.array && c != ']' || !f.array && c != '}' {
				p.pos--
				p.fail()
			}
			v, rec = p.close(f, withRecords)
			p.frames = p.frames[:len(p.frames)-1]
		}
	}
}

// close creates the array or object of the frame f from its members, and removes them from the scratch storage.
func (p *Parser) close(f *frame, withRecords bool) (engine.Value, *Record) {
	cx := p.cx
	vals := p.vals[f.base:]
	var o *engine.Object
	if f.array {
		o = cx.NewArrayCopy(vals)
	} else {
		o = cx.NewObjectFromProperties(p.keys[f.keyBase:], vals)
	}
	v := storage.ObjectValue(o)
	var rec *Record
	if withRecords {
		rec = &Record{value: v}
		recs := append([]*Record(nil), p.recs[f.base:]...)
		if f.array {
			rec.elems = recs
		} else {
			rec.keys = append([]engine.PropKey(nil), p.keys[f.keyBase:]...)
			rec.entries = recs
		}
		clear(p.recs[f.base:])
		p.recs = p.recs[:f.base]
	}
	clear(vals)
	p.vals = p.vals[:f.base]
	p.keys = p.keys[:f.keyBase]
	return v, rec
}
