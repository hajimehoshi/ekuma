// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads that found a property on a prototype see its value after other properties of the prototype were deleted"
---*/

function read(v) { return v.last; }
function call(v) { return v.method(); }

// A prototype with a shared shape gets a new layout without the deleted property.
var shared = { first: 0, last: "a1", method: function () { return "m1"; } };
var a = Object.create(shared);
assert.sameValue(read(a), "a1");
assert.sameValue(call(a), "m1");
delete shared.first;
shared.last = "a2";
shared.method = function () { return "m2"; };
assert.sameValue(read(a), "a2");
assert.sameValue(call(a), "m2");

// A prototype with a shape of its own moves the remaining properties together once the deleted
// ones outnumber them.
var own = {};
for (var i = 0; i < 300; i++) {
	own["p" + i] = i;
}
own.last = "b1";
own.method = function () { return "n1"; };
var b = Object.create(own);
assert.sameValue(read(b), "b1");
assert.sameValue(call(b), "n1");
for (var i = 0; i < 300; i++) {
	delete own["p" + i];
}
own.last = "b2";
own.method = function () { return "n2"; };
assert.sameValue(read(b), "b2");
assert.sameValue(call(b), "n2");
