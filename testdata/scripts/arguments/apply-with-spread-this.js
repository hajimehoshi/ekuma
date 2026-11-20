// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An apply call expands a spread before the arguments object"
---*/

function invoke(o) {
	return o.apply(...[1, 2], arguments);
}

const receiver = { apply(a, b, args) { return a + b + args.length; } };
assert.sameValue(invoke(receiver), 4);
