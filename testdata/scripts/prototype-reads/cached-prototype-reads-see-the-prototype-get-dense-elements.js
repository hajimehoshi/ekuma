// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads that found a property on a prototype see its value after the array index properties of the prototype became elements"
---*/

// An index far beyond the elements is stored like a named property, until enough elements
// come before it. The last of these 256 elements is the one after which the index moves into
// the elements, so nothing else changes the prototype afterwards.
function addElements(o) {
	for (var i = 0; i < 256; i++) {
		o[i] = i;
	}
}

var proto = { x: "x1" };
proto[1000] = 0;
var child = Object.create(proto);
function readX(v) { return v.x; }
assert.sameValue(readX(child), "x1");
assert.sameValue(readX(child), "x1");
addElements(proto);
proto.x = "x2";
assert.sameValue(readX(child), "x2");

// The properties that Array.prototype starts with keep their layout when its index properties
// become elements.
Array.prototype[1000] = 0;
var array = [];
function readJoin(v) { return v.join; }
var join = Array.prototype.join;
assert.sameValue(readJoin(array), join);
assert.sameValue(readJoin(array), join);
addElements(Array.prototype);
function replacement() {}
Array.prototype.join = replacement;
assert.sameValue(readJoin(array), replacement);
Array.prototype.join = join;
