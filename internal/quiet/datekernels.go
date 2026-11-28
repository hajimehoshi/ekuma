// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

var hasDateTimeZoneNameHook bool

// SetDateTimeZoneNameHook records whether Date formatting calls a name hook.
func SetDateTimeZoneNameHook(present bool) {
	hasDateTimeZoneNameHook = present
}

func DateNowKernel(q Context, this storage.Value) (storage.Value, bool) {
	// A host clock can reenter JavaScript and observe the native call frame.
	ms, ok := q.SystemUTCEpochMilliseconds()
	return storage.Number(ms), ok
}

func DateParseKernel(q Context, this, text storage.Value) (storage.Value, bool) {
	if !text.IsString() || text.StringByteLength() > 256 {
		return storage.Undefined, false
	}
	tz, ok := q.DefaultTimeZone()
	t := datetime.Parse(string(text.Str()), tz)
	if !ok && math.IsNaN(t) {
		return storage.Undefined, false
	}
	return storage.Number(t), true
}

func DateToISOStringKernel(q Context, this storage.Value) (storage.Value, bool) {
	t, ok := storage.ThisTimeValue(this)
	if !ok || math.IsNaN(t) {
		return storage.Undefined, false
	}
	var buf [32]byte
	return q.NewASCIIStringBytes(datetime.AppendISOString(buf[:0], t)), true
}

func DateToUTCStringKernel(q Context, this storage.Value) (storage.Value, bool) {
	t, ok := storage.ThisTimeValue(this)
	if !ok {
		return storage.Undefined, false
	}
	if math.IsNaN(t) {
		return q.NewString("Invalid Date"), true
	}
	var buf [40]byte
	return q.NewASCIIStringBytes(datetime.AppendUTCString(buf[:0], t)), true
}

func DateToStringKernel(q Context, this storage.Value) (storage.Value, bool) {
	t, ok := storage.ThisTimeValue(this)
	if !ok {
		return storage.Undefined, false
	}
	if math.IsNaN(t) {
		return q.NewString("Invalid Date"), true
	}
	tz, ok := q.DefaultTimeZone()
	if !ok || hasDateTimeZoneNameHook {
		return storage.Undefined, false
	}
	offset, name := tz.Offset(t)
	var buf [64]byte
	b := datetime.AppendString(buf[:0], t, offset, name)
	if wtf8.IsASCII(wtf8.String(b)) {
		return q.NewASCIIStringBytes(b), true
	}
	return q.NewString(wtf8.FromValidUTF8(string(b))), true
}
