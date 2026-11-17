// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "order"
flags: [noStrict]
---*/

// The metadata property is defined on the class that the class decorators returned,
// after they applied and before the static elements and the initializers run.
var out = [], orig, m;
function has(v) { return Object.hasOwn(v, Symbol.metadata); }
@((v, c) => { c.addInitializer(function () { out.push("class init " + has(this)); }); })
@((v, c) => { out.push("class " + has(v)); orig = v; m = c.metadata; return class extends v {}; })
class C {
	@((v, c) => { c.addInitializer(function () { out.push("method init " + has(this)); }); })
	static m() {}
	static f = out.push("field " + (C[Symbol.metadata] === m));
	static { out.push("block " + (this[Symbol.metadata] === m)); }
}
out.push(has(orig), C[Symbol.metadata] === m);
assert.sameValue(String(out.join()), "class false,method init true,field true,block true,class init true,false,true");
