// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a stack trace shows the names as they were when the error was created"
flags: [noStrict]
---*/

function Thing() {}
Thing.prototype.make = function make() {
	return new Error("m");
};
var thing = new Thing();
function head(e) {
	return e.stack.split("\n").slice(0, 2).join("\n");
}

var early = thing.make();
var reference = head(thing.make());
Object.defineProperty(Thing, "name", {value: "Renamed"});
Error.prototype.name = "Renamed";
assert.sameValue(head(early), reference);
assert.sameValue(head(thing.make()) === reference, false);
