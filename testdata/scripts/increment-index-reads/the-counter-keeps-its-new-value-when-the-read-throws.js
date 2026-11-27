// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a[++i] and a[i++] leave the counter updated when the read throws, also after reads that did not"
---*/

function prefix(a, n) {
	var i = -1, v = [];
	try {
		for (var k = 0; k < n; k++) v.push(a[++i]);
	} catch (e) {
		v.push(e.constructor.name);
	}
	return v.join() + " i=" + i;
}
function postfix(a, n) {
	var i = 0, v = [];
	try {
		for (var k = 0; k < n; k++) v.push(a[i++]);
	} catch (e) {
		v.push(e.constructor.name);
	}
	return v.join() + " i=" + i;
}

var getter = [1, 2, 3, 4];
Object.defineProperty(getter, 2, { get() { throw new EvalError("g"); } });
for (var n = 0; n < 3; n++) {
	assert.sameValue(prefix(getter, 4), "1,2,EvalError i=2");
	assert.sameValue(prefix(null, 1), "TypeError i=0");
	assert.sameValue(postfix(getter, 4), "1,2,EvalError i=3");
	assert.sameValue(postfix(null, 1), "TypeError i=1");
}
