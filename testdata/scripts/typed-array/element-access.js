// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "element access"
---*/

// Reads with constant and computed indices, the last one out of range.
function get(a, i) {
	return [a[0], a[1], a[i], a[i + 1], a[i + 0.5]].join();
}

function show(x) {
	return Object.is(x, -0) ? "-0" : String(x);
}

// Each kind of element, and Int16Array for those that are read another way.
assert.sameValue([
	get(new Int32Array([-5, 7]), 1),
	get(new Float64Array([1.5, -0.25]), 1),
	get(new Uint8Array([200, 3]), 1),
	get(new Uint8ClampedArray([255, 4]), 1),
	get(new Int16Array([-2, 9]), 1),
].join(" "), "-5,7,7,, 1.5,-0.25,-0.25,, 200,3,3,, 255,4,4,, -2,9,9,,");

// Views that start after the beginning of their buffer.
function viewRead(T) {
	return get(new T(new T([1, 2, 3]).buffer, T.BYTES_PER_ELEMENT), 1);
}
function viewWrite(T) {
	var base = new T(3), view = new T(base.buffer, T.BYTES_PER_ELEMENT, 1);
	view[0] = 5;
	view[1] = 6;
	return base.join();
}
assert.sameValue([Int32Array, Float64Array, Uint8Array].map(viewRead).join(" "), "2,3,3,, 2,3,3,, 2,3,3,,");
assert.sameValue([Int32Array, Float64Array, Uint8Array].map(viewWrite).join(" "), "0,5,0 0,5,0 0,5,0");

// A view that tracks the length of its buffer, and a fixed-length one that the shrinking of its
// buffer leaves out of bounds although the bytes of its first element remain.
var tracking = new Int32Array(new ArrayBuffer(8, {maxByteLength: 16}));
tracking[0] = 1;
tracking[1] = 2;
var rab = new ArrayBuffer(12, {maxByteLength: 16}), fixed = new Int32Array(rab, 4, 2);
fixed[0] = 8;
fixed[1] = 9;
var r = [get(tracking, 1), get(fixed, 1)];
rab.resize(8);
fixed[0] = 3;
r.push(get(fixed, 1));
rab.resize(12);
r.push(get(fixed, 1));
assert.sameValue(r.join(" "), "1,2,2,, 8,9,9,, ,,,, 8,0,0,,");

// A detached buffer, and a typed array that has side data as the key of a WeakMap.
var detached = new Int32Array([1, 2]);
detached.buffer.transfer();
detached[0] = 5;
var key = new Float64Array([4, 5]);
new WeakMap().set(key, 1);
key[1] = 6;
assert.sameValue([get(detached, 1), get(key, 1)].join(" "), ",,,, 4,6,6,,");

// The conversions of stored values.
function store(a, values) {
	var out = [];
	for (var v of values) {
		a[1] = v;
		out.push(show(a[1]));
	}
	return out.join();
}
assert.sameValue([
	store(new Int32Array(2), [-1.5, 2 ** 31, -(2 ** 31) - 1, NaN, "6", 7.9]),
	store(new Uint8Array(2), [256, -1, 1.9, 255]),
	store(new Uint8ClampedArray(2), [300, 1.5, -3]),
	store(new Float64Array(2), [0.1, -0, NaN]),
].join(" "), "-1,-2147483648,2147483647,0,6,7 0,255,1,255 255,2,0 0.1,-0,NaN");
