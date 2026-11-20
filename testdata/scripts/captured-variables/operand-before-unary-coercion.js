// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An operand keeps its value while unary minus converts the next operand"
---*/

function subtractObject() {
	let x = 1;
	const o = { valueOf() { x = 2; return 3; } };
	return x + -o;
}

assert.sameValue(subtractObject(), -2);
