// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "replaced class"
flags: [noStrict]
---*/

// The class that a class decorator returns gets the class binding and the static
// fields, and is the this value of the static blocks and the initializers.
var orig, inner, out = [];
@((v, c) => { c.addInitializer(function () { out.push(this === inner); }); })
@((v, c) => { orig = v; return class extends v {}; })
class C {
	static s = this;
	static { inner = C; out.push(this === C); }
}
out.push(C === inner, C !== orig, Object.hasOwn(C, "s"), Object.hasOwn(orig, "s"));
assert.sameValue(String(out.join()), "true,true,true,true,true,false");
