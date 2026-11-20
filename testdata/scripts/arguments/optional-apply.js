// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An optional apply call checks the method before evaluating the arguments"
---*/

let calls = 0;
function invoke(o) {
	return o.apply?.(++calls, arguments);
}

assert.sameValue(invoke({}), undefined);
assert.sameValue(calls, 0);
const receiver = { apply(n, args) { return this === receiver && args[0] === this && n === 1; } };
assert.sameValue(invoke(receiver), true);
