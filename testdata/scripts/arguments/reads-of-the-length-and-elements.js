// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Reads of the length and the elements of arguments see the arguments of the call, and other keys see none"
---*/

function elements(a, b) {
	return [arguments.length, arguments[0], arguments[1], arguments[2], arguments[3], arguments[-0]].join();
}
assert.sameValue(elements(), "0,,,,,");
assert.sameValue(elements(1), "1,1,,,,1");
assert.sameValue(elements(1, 2, 3, 4), "4,1,2,3,4,1");

// Each call reads one key, since a key that is not the index of an argument can make the
// reads after it take another path.
function at(k, a, b) {
	return arguments[k];
}
assert.sameValue(at("1", "x", "y"), "x");
assert.sameValue(at(true, "x", "y"), undefined);
assert.sameValue(at(1.5, "x", "y"), undefined);
assert.sameValue(at(NaN, "x", "y"), undefined);
assert.sameValue(at(-1, "x", "y"), undefined);
assert.sameValue(at(3, "x", "y"), undefined);
