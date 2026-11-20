// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a new own property shadows a cached read on a receiver that lost a property"
---*/

function read(o) { return o.x; }

function check(proto, inherited) {
	var o = Object.create(proto);
	o.temporary = 0;
	delete o.temporary;
	assert.sameValue(read(o), inherited);
	Object.defineProperty(o, "x", {value: "own"});
	assert.sameValue(read(o), "own");
}

check({}, undefined);
check({x: "data"}, "data");
check({get x() { return "getter"; }}, "getter");
