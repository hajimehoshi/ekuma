// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array rest prepares its target before reading the live iterator"
---*/

var source = [0, , 2], target = {}, reads = 0;
var proto = Object.create(Array.prototype, {
	1: {get() {
		reads++;
		source.push(3);
		return 1;
	}},
});
function prepare() {
	Object.setPrototypeOf(source, proto);
	return "rest";
}
[, ...target[prepare()]] = source;
assert.sameValue(target.rest.join(), "1,2,3");
assert.sameValue(reads, 1);

var marker = {}, caught, closed = false, rest = "previous";
var iterable = {
	[Symbol.iterator]() {
		return {
			next() { return {get value() { throw marker; }}; },
			return() { closed = true; return {}; },
		};
	},
};
try {
	[...rest] = iterable;
} catch (e) {
	caught = e;
}
assert.sameValue(caught, marker);
assert.sameValue(closed, false);
assert.sameValue(rest, "previous");
