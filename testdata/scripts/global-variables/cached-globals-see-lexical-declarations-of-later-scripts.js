// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads and assignments of global variables that ran before a later script declared global lexical bindings see the bindings afterwards"
---*/

globalThis.shadowed = "property";
function readShadowed() { return shadowed; }
function writeShadowed(v) { shadowed = v; }
writeShadowed("property 2");
assert.sameValue(readShadowed(), "property 2");
$262.evalScript('let shadowed = "binding";');
assert.sameValue(readShadowed(), "binding");
writeShadowed("binding 2");
assert.sameValue(globalThis.shadowed, "property 2");
assert.sameValue(readShadowed(), "binding 2");

// A binding that was read keeps its value while later scripts declare many more.
$262.evalScript('let early = 1;');
function readEarly() { return early; }
function writeEarly(v) { early = v; }
writeEarly(2);
assert.sameValue(readEarly(), 2);
var lets = "";
for (var i = 0; i < 200; i++) {
	lets += "let late" + i + " = " + i + ";";
}
$262.evalScript(lets);
writeEarly(3);
assert.sameValue(new Function("return early;")(), 3);
$262.evalScript("early = 4;");
assert.sameValue(readEarly(), 4);
