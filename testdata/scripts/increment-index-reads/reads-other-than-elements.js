// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a[++i] and a[i++] read holes through the prototype chain, see arrays that a getter changes, and step large and negative zero counters as other updates do"
---*/

function prefix(a, i, n) {
	var v = [];
	for (var k = 0; k < n; k++) v.push(a[++i]);
	return v.map(String).join() + " i=" + i;
}
function postfix(a, i, n) {
	var v = [];
	for (var k = 0; k < n; k++) v.push(a[i++]);
	return v.map(String).join() + " i=" + i;
}

for (var n = 0; n < 3; n++) {
	Array.prototype[1] = "array";
	Object.prototype[3] = "object";
	assert.sameValue(prefix([1, , 3, , 5], -1, 5), "1,array,3,object,5 i=4");
	assert.sameValue(postfix([1, , 3, , 5], 0, 5), "1,array,3,object,5 i=5");
	delete Array.prototype[1];
	delete Object.prototype[3];

	// 2 ** 53 + 1 is 2 ** 53, so the postfix read uses the same key twice.
	assert.sameValue(postfix({ "9007199254740992": "2^53" }, 2 ** 53, 2), "2^53,2^53 i=9007199254740992");
	assert.sameValue(postfix([1, 2], -0, 1), "1 i=1");

	var shrink = [1, 2, 3, 4, 5];
	Object.defineProperty(shrink, 1, { get() { shrink.length = 3; return "shrunk"; }, configurable: true });
	assert.sameValue(prefix(shrink, -1, 5), "1,shrunk,3,undefined,undefined i=4");
	var holes = [1, 2, 3, 4];
	Object.defineProperty(holes, 1, { get() { delete holes[2]; return "deleted"; }, configurable: true });
	assert.sameValue(postfix(holes, 0, 4), "1,deleted,undefined,4 i=4");
}
