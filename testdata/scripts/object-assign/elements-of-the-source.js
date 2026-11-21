// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an object spread gives the copy elements of its own, and Object.assign sets elements through a setter that the target inherits"
---*/

const src = { a: 1, b: 2 };
for (let i = 0; i < 4; i++) {
	src[i] = i * 10;
}
const copy = { ...src };
assert.sameValue(Object.keys(copy).join(), "0,1,2,3,a,b");
assert.sameValue(copy[2], 20);
copy[0] = 99;
copy[4] = 40;
assert.sameValue(src[0], 0);
assert.sameValue(4 in src, false);

const log = [];
Object.defineProperty(Object.prototype, "1", {
	set(v) {
		log.push(v);
	},
	configurable: true,
});
assert.sameValue({ ...src }.hasOwnProperty(1), true);
assert.sameValue(Object.assign({}, src).hasOwnProperty(1), false);
assert.sameValue(log.join(), "10");
delete Object.prototype[1];
