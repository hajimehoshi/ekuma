// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "anonymous functions named after keys known at run time"
flags: [noStrict]
---*/

var k = "a";
class C { [k] = () => {}; 128 = class { static n = this.name; }; }
var c = new C();
var o = { [k]: class { static n = this.name; } };
assert.sameValue(String([c.a.name, c[128].n, o.a.n].join()), "a,128,a");
