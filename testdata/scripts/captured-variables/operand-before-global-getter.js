// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An operand keeps its value while a global identifier invokes a getter"
---*/

function addGlobal() {
	let x = 1;
	Object.defineProperty(globalThis, "globalOperand", {
		get() { x = 2; return 3; }
	});
	return x + globalOperand;
}

assert.sameValue(addGlobal(), 4);
