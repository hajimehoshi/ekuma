// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Typed array and DataView methods see a buffer that user code resizes"
---*/

// check compares what f returns, as JSON with "undefined" for undefined, or the name of the error
// that it throws.
function check(name, expected, f) {
	let actual;
	try {
		actual = JSON.stringify(f(), (key, value) => value === undefined ? "undefined" : value);
	} catch (e) {
		actual = e.name;
	}
	assert.sameValue(actual, expected, name);
}

check("fill with a shrinking value", "[7,7,7,7]", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	t.fill({ valueOf() { buffer.resize(4); return 7; } });
	return [...t];
});
check("fill of a view that goes out of bounds", "TypeError", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	const t = new Uint8Array(buffer, 0, 8);
	t.fill(1, 0, { valueOf() { buffer.resize(4); return 8; } });
});
check("set with a shrinking element", "[1,2]", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	t.set([1, { valueOf() { buffer.resize(2); return 2; } }, 3]);
	return [...t];
});
check("copyWithin with a shrinking start", "[1,2,1,2,3,4]", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	t.set([1, 2, 3, 4, 5, 6, 7, 8]);
	t.copyWithin(2, { valueOf() { buffer.resize(6); return 0; } });
	return [...t];
});
check("slice with a shrinking start", "[2,3,0,0,0,0,0]", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	t.set([1, 2, 3, 4, 5, 6, 7, 8]);
	return [...t.slice({ valueOf() { buffer.resize(3); return 1; } })];
});
check("with and a growing value", "[0,7,0,0]", function () {
	const buffer = new ArrayBuffer(4, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	return [...t.with(1, { valueOf() { buffer.resize(8); return 7; } })];
});
check("includes with a shrinking start", "true", function () {
	const buffer = new ArrayBuffer(4, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	return t.includes(undefined, { valueOf() { buffer.resize(1); return 0; } });
});
check("sort with a shrinking comparator", "[1,2]", function () {
	const buffer = new ArrayBuffer(4, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	t.set([4, 3, 2, 1]);
	t.sort((x, y) => { buffer.resize(2); return x - y; });
	return [...t];
});
check("subarray with a growing end", "4", function () {
	const buffer = new ArrayBuffer(4, { maxByteLength: 16 });
	const t = new Uint8Array(buffer);
	return t.subarray(0, { valueOf() { buffer.resize(8); return 4; } }).length;
});
check("setUint8 with a shrinking value", "RangeError", function () {
	const buffer = new ArrayBuffer(8, { maxByteLength: 16 });
	new DataView(buffer).setUint8(6, { valueOf() { buffer.resize(2); return 1; } });
});
