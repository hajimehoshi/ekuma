// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "length"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original length getter of typed arrays"
---*/

// The lengths read at warm sites follow the buffers and the properties: a fixed-length view
// that a shrinking leaves out of bounds, a view that tracks the length of its buffer, a detached
// buffer, a typed array with side data as the key of a WeakMap, one that gets an own length,
// and one of a class with a getter of its own.
class Sub extends Uint8Array {
	get length() { return 99; }
}
var rab = new ArrayBuffer(16, {maxByteLength: 32});
var fixed = new Int32Array(rab, 4, 2), tracking = new Int32Array(rab), detached = new Float64Array(2);
var key = new Uint16Array(3), own = new Uint8Array(2), sub = new Sub(2);
new WeakMap().set(key, 0);
function lengths() {
	return [fixed.length, tracking.length, detached.length, key.length, own.length, sub.length].join();
}
var r = [];
for (var i = 0; i < 3; i++) r.push(lengths());
rab.resize(8);
detached.buffer.transfer();
Object.defineProperty(own, "length", {value: 5});
r.push(lengths());
rab.resize(32);
r.push(lengths());
assert.sameValue(r.join(" "), "2,4,2,3,2,99 2,4,2,3,2,99 2,4,2,3,2,99 0,2,0,3,5,99 2,8,0,3,5,99");

// The length of a typed array keeps following its getter and own properties
// after repeated reads of the same site.
var a = new Uint8Array(4);
r = [];
function len(x) { return x.length; }
for (var i = 0; i < 10; i++) len(a);
r.push(len(a));
Object.defineProperty(Object.getPrototypeOf(Uint8Array.prototype), "length", {get() { return 7; }});
r.push(len(a));
Object.defineProperty(a, "length", {value: 9});
r.push(len(a));
assert.sameValue(String(r.join()), "4,7,9");
