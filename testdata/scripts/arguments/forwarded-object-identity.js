// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Calls to a custom apply method share the arguments object of the activation"
---*/

const receiver = { apply(_, args) { return args; } };

function capture() {
	const first = receiver.apply(null, arguments);
	assert.sameValue(receiver.apply(null, arguments), first);
	return first;
}

assert.sameValue(capture() === capture(), false);
