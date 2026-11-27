// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// newTail returns a string built by appends, which is the tail of its builder, and its contents.
func newTail(t *testing.T, cx *engine.ExecutionContext) (storage.Value, wtf8.String) {
	t.Helper()
	acc := cx.NewString("")
	var want wtf8.String
	for range 64 {
		acc = cx.Concat(acc, cx.NewString("ab"))
		want += "ab"
	}
	if storage.TailBuilder(acc.O) == nil {
		t.Fatal("the appends made no tail of a builder")
	}
	return acc, want
}

// TestConcatShortSuffix tests appending a string short enough to be packed to the tail of a
// builder.
func TestConcatShortSuffix(t *testing.T) {
	t.Run("ASCII", func(t *testing.T) {
		cx, _ := engine.NewExecutionContext(nil)
		tail, want := newTail(t, cx)
		suffix := cx.NewString("abcdefgh")
		// Only the result allocates.
		if n := testing.AllocsPerRun(10, func() { tail = cx.Concat(tail, suffix) }); n != 1 {
			t.Errorf("allocations per append: got %v, want 1", n)
		}
		want += wtf8.String(strings.Repeat("abcdefgh", 11))
		if got := tail.Str(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got := tail.StringLength(); got != len(want) {
			t.Errorf("length: got %d, want %d", got, len(want))
		}
		if storage.TailBuilder(tail.O) == nil {
			t.Error("the result is not the tail of the builder")
		}
	})
	t.Run("non-ASCII", func(t *testing.T) {
		cx, _ := engine.NewExecutionContext(nil)
		tail, want := newTail(t, cx)
		// The second half of a surrogate pair joins the first half at the end of the tail.
		tail = cx.Concat(tail, cx.NewString("\xed\xa0\xbd"))
		tail = cx.Concat(tail, cx.NewString("\xed\xb8\x80"))
		want += "\U0001f600"
		if got := tail.Str(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got, want := tail.StringLength(), len(want)-4+2; got != want {
			t.Errorf("length: got %d, want %d", got, want)
		}
	})
	t.Run("empty", func(t *testing.T) {
		cx, _ := engine.NewExecutionContext(nil)
		tail, want := newTail(t, cx)
		empty := cx.NewString("")
		var got storage.Value
		if n := testing.AllocsPerRun(10, func() { got = cx.Concat(tail, empty) }); n != 0 {
			t.Errorf("allocations per append: got %v, want 0", n)
		}
		if got.Str() != want {
			t.Errorf("got %q, want %q", got.Str(), want)
		}
		if storage.TailBuilder(tail.O) == nil {
			t.Error("the string is no longer the tail of the builder")
		}
	})
}

// TestConcatShortSuffixLengthLimit tests that appending a short string to a tail fails at the
// string length limit.
func TestConcatShortSuffixLengthLimit(t *testing.T) {
	if runtime.GOOS == "js" {
		// The memory of wasm never shrinks, so the tests that follow would lose it.
		t.Skip("a tail at the string length limit takes 1 GiB")
	}
	// The memory of the tail goes back before the tests that follow.
	t.Cleanup(runtime.GC)
	cx := newTestContext(t)
	sb := new(strings.Builder)
	sb.Grow(storage.MaxStringLength)
	chunk := strings.Repeat("a", 1<<20)
	for n := storage.MaxStringLength - 4; sb.Len() < n; {
		sb.WriteString(chunk[:min(len(chunk), n-sb.Len())])
	}
	tail := cx.NewBufferedString(wtf8.String(sb.String()), storage.ObjASCIIKnown|storage.ObjASCII, sb, nil)
	appendTo := cx.NewFunction("appendTo", func(cx *engine.ExecutionContext, _ storage.Value, args []storage.Value) storage.Value {
		tail = cx.Concat(tail, args[0])
		return storage.Undefined
	})
	if _, err := cx.HostCall(appendTo, storage.Undefined, []storage.Value{cx.NewString("abcd")}); err != nil {
		t.Errorf("appending up to the limit: %v", err)
	}
	if got := tail.StringByteLength(); got != storage.MaxStringLength {
		t.Errorf("length: got %d, want %d", got, storage.MaxStringLength)
	}
	_, err := cx.HostCall(appendTo, storage.Undefined, []storage.Value{cx.NewString("a")})
	if ex, ok := errors.AsType[*storage.Error](err); !ok || cx.ProtoOf(ex.Value.Object()) != cx.Intrinsic(storage.IntrinsicRangeErrorPrototype) {
		t.Errorf("appending past the limit: got %v, want a RangeError", err)
	}
	tail = storage.Undefined
}
