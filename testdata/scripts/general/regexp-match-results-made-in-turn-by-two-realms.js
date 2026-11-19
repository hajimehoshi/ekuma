// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "match results made in turn by two realms get the properties and the Array.prototype of the realm of exec"
flags: [noStrict]
---*/

var other = $262.createRealm().global;
function check(g, flags) {
	const m = g.RegExp.prototype.exec.call(new g.RegExp("(?<x>b)", flags), "ab");
	return (Object.getPrototypeOf(m) === g.Array.prototype) + ":" + Object.keys(m);
}
var plain = "true:0,1,index,input,groups";
var withIndices = "true:0,1,index,input,groups,indices";
assert.sameValue(check(globalThis, ""), plain);
assert.sameValue(check(other, ""), plain);
assert.sameValue(check(globalThis, "d"), withIndices);
assert.sameValue(check(other, "d"), withIndices);
assert.sameValue(check(globalThis, ""), plain);
