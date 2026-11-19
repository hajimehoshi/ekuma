// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a call of a method read from a local or this runs once, with or without arguments"
---*/

function counter() {
	var n = 0;
	return {
		next() { return ++n; },
		add(d) { return n += d; },
		viaThis() { return this.next(); },
	};
}
function twice(o) { o.next(); return o.next(); }
function viaThis(o) { return o.viaThis(); }
function popTwo(a) { a.pop(); return a.pop(); }
function addTen(o, d) { o.add(d); return o.add(10 - d); }
var c = counter();
for (var k = 1; k <= 5; k++) {
	assert.sameValue(twice(c), 3 * k - 1);
	assert.sameValue(viaThis(c), 3 * k);
	assert.sameValue(addTen(c, 4), 3 * k + 10);
	c.add(-10);
	var a = [1, 2, 3, 4];
	assert.sameValue(popTwo(a), 3);
	assert.sameValue(a.length, 2);
}
