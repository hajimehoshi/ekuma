// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a native function of another realm throws in its realm, whether script or a native function calls it"
---*/

const foreign = $262.createRealm().global;
const callerPrototype = Object.prototype;
let fromScript;
try {
	foreign.JSON.parse("{");
} catch (e) {
	fromScript = e;
	assert.sameValue(Object.getPrototypeOf({}), callerPrototype);
}
assert.sameValue(Object.getPrototypeOf(fromScript), foreign.SyntaxError.prototype);

let fromNative;
try {
	["{"].forEach(foreign.JSON.parse);
} catch (e) {
	fromNative = e;
}
assert.sameValue(Object.getPrototypeOf(fromNative), foreign.SyntaxError.prototype);
