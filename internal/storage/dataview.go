// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// NewDataViewObject returns a DataView with the prototype proto on the buffer object buffer
// from byteOffset on. byteLength is -1 for a view whose length tracks the length of the buffer.
func (r *RealmPart) NewDataViewObject(proto *Object, buffer *Object, byteOffset, byteLength int) *Object {
	return NewObjectWithState(r.ShapeFor(proto), ClassDataView, &DataView{
		buffer:   buffer,
		data:     buffer.ExtState().(*ArrayBuffer),
		offset:   byteOffset,
		length:   max(byteLength, 0),
		tracking: byteLength < 0,
	})
}

// DataView is the state of a DataView object.
type DataView struct {
	buffer   *Object
	data     *ArrayBuffer
	offset   int
	length   int
	tracking bool
}

// DataViewData returns the state of the DataView v, or nil when v is not a DataView.
func DataViewData(v Value) *DataView {
	if v.IsObject() && v.O.Class == ClassDataView {
		return v.O.ExtState().(*DataView)
	}
	return nil
}

// Buffer returns [[ViewedArrayBuffer]] of d.
func (d *DataView) Buffer() *Object {
	return d.buffer
}

// ByteOffset returns [[ByteOffset]] of d.
func (d *DataView) ByteOffset() int {
	return d.offset
}

// ByteLength implements GetViewByteLength. It reports false when the view is out of bounds,
// which includes a detached buffer.
func (d *DataView) ByteLength() (int, bool) {
	if d.data.state == bufferDetached {
		return 0, false
	}
	return d.byteLengthOf(d.data.ByteLength())
}

// byteLengthOf implements GetViewByteLength for a buffer of n bytes that is not detached.
func (d *DataView) byteLengthOf(n int) (int, bool) {
	if d.tracking {
		if d.offset > n {
			return 0, false
		}
		return n - d.offset, true
	}
	if d.offset+d.length > n {
		return 0, false
	}
	return d.length, true
}

// Lock locks the buffer as [ArrayBuffer.Lock] does and returns the bytes that d views.
// It reports false when the buffer is detached or d is out of bounds. Always pair Lock with
// [DataView.Unlock]. The slice must not be modified when the buffer is immutable.
func (d *DataView) Lock() ([]byte, bool) {
	b := d.data.Lock()
	if d.data.state == bufferDetached {
		return nil, false
	}
	n, ok := d.byteLengthOf(len(b))
	if !ok {
		return nil, false
	}
	return b[d.offset : d.offset+n], true
}

// Unlock ends the access of [DataView.Lock].
func (d *DataView) Unlock() {
	d.data.Unlock()
}
