// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Subsequent apply and spread calls use the exposed arguments object"
flags: [onlyStrict]
---*/

function first(x) { return x; }
const receiver = { apply(_, args) { args[0] = 7; return args; } };

function invoke(x) {
	const args = receiver.apply(null, arguments);
	assert.sameValue(first.apply(null, arguments), 7);
	assert.sameValue(first(...arguments), 7);
	args[Symbol.iterator] = function* () { yield 9; };
	return first(...arguments);
}

assert.sameValue(invoke(1), 9);
