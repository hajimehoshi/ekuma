// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a native function of another realm throws in its realm, whether script or a native function calls it"
---*/

const foreign = $262.createRealm().global;
let fromScript;
try {
	foreign.JSON.parse("{");
} catch (e) {
	fromScript = e;
}
assert.sameValue(fromScript instanceof foreign.SyntaxError, true);

let fromNative;
try {
	["{"].forEach(foreign.JSON.parse);
} catch (e) {
	fromNative = e;
}
assert.sameValue(fromNative instanceof foreign.SyntaxError, true);
