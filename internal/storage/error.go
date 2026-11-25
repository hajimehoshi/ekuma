// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Error is a JavaScript exception that reached Go.
type Error struct {
	// Value is the thrown value.
	Value Value

	msg string
}

func (e *Error) Error() string {
	return e.msg
}

// ErrorOf returns the Go error for the thrown value v.
func (sp *Space) ErrorOf(v Value) *Error {
	var msg string
	if v.IsObject() {
		if s, ok := DiagnosticDataProperty(v.O, sp.Key(KeyStack)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else if s, ok := ErrorStack(v); ok {
			msg = wtf8.ToValidUTF8(s)
		} else if s, ok := DiagnosticDataProperty(v.O, sp.Key(KeyMessage)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else {
			msg = "uncaught exception"
		}
	} else {
		msg = "uncaught " + DisplayString(v)
	}
	return &Error{
		Value: v,
		msg:   msg,
	}
}

// ErrorData is the internal state of an error object that has a recorded stack trace.
type ErrorData struct {
	stack wtf8.String

	// Pending is set until stack is formatted from name, msg and calls.
	Pending bool

	// Name in WTF-8 and msg in UTF-8 begin the stack trace, and calls are its lines.
	Name  wtf8.String
	Msg   string
	Calls []StackCall

	// GoErr is the Go error that the error object was made from, or nil.
	GoErr error
}

// IsError reports whether v is an object with an [[ErrorData]] internal slot.
func IsError(v Value) bool {
	return v.IsObject() && v.O.Class == ClassError
}

// ErrorStack returns the stack trace that agent.RecordStack recorded for the error object v.
// It reports false when v has no [[ErrorData]] internal slot or no recorded stack trace.
func ErrorStack(v Value) (wtf8.String, bool) {
	if !IsError(v) {
		return "", false
	}
	d, ok := v.O.ExtState().(*ErrorData)
	if !ok {
		return "", false
	}
	if d.Pending {
		d.stack = formatStack(d.Name, d.Msg, d.Calls)
		d.Pending = false
		d.Name, d.Msg, d.Calls = "", "", nil
	}
	return d.stack, true
}

// GoError returns the Go error that v was made from by ExecutionContext.NewGoError, or nil when it was not.
func GoError(v Value) error {
	if !IsError(v) {
		return nil
	}
	if d, ok := v.O.ExtState().(*ErrorData); ok {
		return d.GoErr
	}
	return nil
}

// StackCall is a line of a stack trace.
type StackCall struct {
	// Fp is the function that runs, or nil for a native function.
	Fp *FuncProto

	// Name is the Name of the function.
	Name wtf8.String

	// Recv is the type name of the receiver of a method call, or empty.
	Recv wtf8.String

	// Pc is past the instruction of fp that is running.
	Pc int

	IsNew bool
}

const stackFramePrefix = "\n    at "

// formatStack returns a stack trace starting with the WTF-8 name and UTF-8 message,
// followed by one line per call.
func formatStack(name wtf8.String, msg string, calls []StackCall) wtf8.String {
	// ASCII punctuation separates the WTF-8 fields, so surrogate pairs cannot span writes.
	var b strings.Builder
	b.Grow(len(calls) * len(stackFramePrefix))
	b.WriteString(string(name))
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(string(wtf8.FromValidUTF8(msg)))
	}
	for i := range calls {
		calls[i].write(&b)
	}
	return Clip(wtf8.String(b.String()), MaxStringLength, "")
}

// maxFrameName is the length in bytes up to which a stack trace shows a name.
const maxFrameName = 1 << 10

// write writes the line of the stack trace for c to b.
func (c *StackCall) write(b *strings.Builder) {
	b.WriteString(stackFramePrefix)
	name := Clip(c.Name, maxFrameName, "...")
	switch {
	case c.IsNew:
		b.WriteString("new ")
		name = cmp.Or(name, "<anonymous>")
	case c.Recv != "":
		b.WriteString(string(Clip(c.Recv, maxFrameName, "...")))
		b.WriteByte('.')
		name = cmp.Or(name, "<anonymous>")
	}
	if name != "" {
		b.WriteString(string(name))
		if c.Fp != nil && c.Fp.File == "" {
			return
		}
		b.WriteString(" (")
	}
	if fp := c.Fp; fp != nil && fp.File != "" {
		b.WriteString(string(wtf8.FromValidUTF8(fp.File)))
		if line, column := fp.Position(max(c.Pc-1, 0)); line > 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(line))
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(column))
		}
	} else {
		b.WriteString("<anonymous>")
	}
	if name != "" {
		b.WriteByte(')')
	}
}
