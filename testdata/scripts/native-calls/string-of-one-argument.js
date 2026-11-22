// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "String called with one argument of each kind"
---*/

assert.sameValue([String(12), String(-0), String(1.5), String(1e21), String(-1e-7), String(NaN)].join(),
  "12,0,1.5,1e+21,-1e-7,NaN");
var s = "a string longer than eight bytes";
assert.sameValue(String(s), s);
assert.sameValue([String(true), String(null), String(undefined), String(10n), String(Symbol("a"))].join(),
  "true,null,undefined,10,Symbol(a)");
assert.sameValue(String({ toString() { return "object"; } }), "object");
assert.sameValue(typeof new String(5), "object");

// A conversion that runs code runs inside String.
var line;
try {
  String({ toString() { throw new Error("m"); } });
} catch (e) {
  line = e.stack.split("\n")[2];
}
assert.sameValue(line, "    at String (<anonymous>)");
