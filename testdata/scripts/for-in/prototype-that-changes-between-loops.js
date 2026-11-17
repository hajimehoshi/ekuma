// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prototype that changes between loops"
flags: [noStrict]
---*/

var p = Object.defineProperty({}, "h", {value: 0, configurable: true});
var o = Object.create(p);
o.a = 0;
function keys() { var s = ""; for (var k in o) s += k; return s; }
var r = [keys()];
p.x = 0;
r.push(keys());
delete p.x;
r.push(keys());
p.y = 0;
r.push(keys());
Object.defineProperty(p, "h", {enumerable: true});
r.push(keys());
assert.sameValue(String(r.join()), "a,ax,a,ay,ahy");
