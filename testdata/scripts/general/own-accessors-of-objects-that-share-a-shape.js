// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "own accessors of objects that share a shape"
flags: [noStrict]
---*/

function make(d) { return { get x() { return d; }, set x(v) { d += v; } }; }
function read(o) { return String(o.x); }
var r = [];
for (var d = 0; d < 2; d++) { var o = make(d); o.x = 10; r.push(read(o)); }
Object.defineProperty(o, "x", { get() { return "new"; } });
r.push(read(o));
Object.defineProperty(o, "x", { get: undefined });
r.push(read(o)); assert.sameValue(String(r.join()), "10,11,new,undefined");
