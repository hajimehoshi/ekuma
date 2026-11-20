// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "functions of two realms read and assign the global variables of their own realm, also when they call each other"
---*/

var other = $262.createRealm();
other.evalScript('var v = "other"; function readV() { return v; } function writeV(x) { v = x; }');
var v = "main";
function readMain() { return v; }
for (var i = 0; i < 3; i++) {
	assert.sameValue(other.global.readV(), "other");
	assert.sameValue(readMain(), "main");
}
other.global.writeV("changed");
assert.sameValue(other.global.v, "changed");
assert.sameValue(readMain(), "main");
assert.sameValue(other.global.readV(), "changed");
