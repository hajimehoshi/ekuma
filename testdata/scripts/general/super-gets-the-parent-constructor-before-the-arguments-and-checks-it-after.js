// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "super gets the parent constructor before the arguments and checks it after"
flags: [noStrict]
---*/

class B {}
class D extends B { constructor() { super(Object.setPrototypeOf(D, null)); } }
var token = {};
class E extends B { constructor() { super((() => { throw token; })()); } }
Object.setPrototypeOf(E, Math.sin);
var r;
try { new E(); } catch (e) { r = e === token; }
assert.sameValue(String((new D() instanceof B) + "," + r), "true,true");
