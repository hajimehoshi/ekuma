// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "push and pop called as methods of receivers other than plain arrays act on them as the methods do"
---*/

function throwsTypeError(f) {
	try {
		f();
	} catch (e) {
		return e instanceof TypeError;
	}
	return false;
}

const frozen = Object.freeze([1]);
assert.sameValue(throwsTypeError(() => frozen.push(2)), true);
assert.sameValue(throwsTypeError(() => frozen.pop()), true);

const like = { length: 0, push: Array.prototype.push, pop: Array.prototype.pop };
assert.sameValue(like.push(5), 1);
assert.sameValue(like[0], 5);
assert.sameValue(like.pop(), 5);
assert.sameValue(like.length, 0);

let set;
Object.defineProperty(Array.prototype, "0", {
	set(v) {
		set = v;
	},
	configurable: true,
});
const empty = [];
assert.sameValue(empty.push(7), 1);
assert.sameValue(set, 7);
assert.sameValue(empty.hasOwnProperty(0), false);
delete Array.prototype[0];

const holes = [1, , ];
assert.sameValue(holes.pop(), undefined);
assert.sameValue(holes.length, 1);

Number.prototype.push = Array.prototype.push;
Number.prototype.pop = Array.prototype.pop;
assert.sameValue((1).push(9), 1);
assert.sameValue((1).pop(), undefined);
delete Number.prototype.push;
delete Number.prototype.pop;
