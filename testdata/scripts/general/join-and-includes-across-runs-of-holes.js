// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "join and includes across runs of holes"
flags: [noStrict]
---*/

var r = [];
var a = []; a.length = 2 ** 32 - 1; a[3] = "x";
try { a.join("ab".repeat(1000)); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
var s = "\uDC00\uD800";
r.push([, , "c", , ].join(s) === s + s + "c" + s);
var c = [{ toString() { Array.prototype[2] = "p"; return "o"; } }, , , ]; c.length = 4;
r.push(c.join("-")); delete Array.prototype[2];
var d = []; d[100000] = 1;
r.push(d.includes(undefined), d.includes(undefined, 100000));
assert.sameValue(String(r.join()), "true,true,o--p-,true,false");
