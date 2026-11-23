// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a generator whose resumption fails at the nesting limit can be resumed later"
flags: [noStrict]
---*/

var generators = [];
var errors = [];
function* nest() {
	var inner = nest();
	generators.push(inner);
	try {
		inner.next();
	} catch (e) {
		errors.push(e);
	}
	yield 1;
}
nest().next();

assert.sameValue(errors.length, 1);
assert.sameValue(errors[0] instanceof RangeError, true);

// The innermost generator did not start. It is not running, so stepping it does not throw a
// TypeError. Break the recursion first.
nest = function* () {};
var last = generators[generators.length - 1];
var result = last.next();
assert.sameValue(result.value, 1);
assert.sameValue(result.done, false);
assert.sameValue(last.next().done, true);
