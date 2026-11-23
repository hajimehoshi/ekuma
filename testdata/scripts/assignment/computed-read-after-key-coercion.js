// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A computed read keeps its assigned receiver while evaluating the key and preserves the destination if its getter throws"
---*/

function read() {
	let receiver, result = "previous";
	const marker = {};
	const source = { get 0() { throw marker; } };
	const key = { valueOf() { receiver = ["replacement"]; return 0; } };
	try {
		result = (receiver = source)[+key];
	} catch (e) {
		assert.sameValue(e, marker);
		return result;
	}
}

assert.sameValue(read(), "previous");
