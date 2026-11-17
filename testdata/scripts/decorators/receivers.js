// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "receivers"
flags: [onlyStrict]
---*/

// A decorator is called with the object of a member expression as its this value.
var out = [], ns = {
	dec() { out.push(this === ns); },
	make() { return function () { out.push(String(this)); }; },
};
@ns.dec @(ns.dec) @((0, ns.dec)) @ns.make() class C {}
assert.sameValue(String(out.join()), "undefined,false,true,true");
