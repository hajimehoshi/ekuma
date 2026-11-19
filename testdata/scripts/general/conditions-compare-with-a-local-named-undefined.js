// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "conditions compare with a local named undefined"
flags: [noStrict]
---*/

function f(x, undefined) {
	return x === undefined ? "equal" : "different";
}
assert.sameValue(f(1, 1), "equal");
assert.sameValue(f(void 0, 1), "different");
