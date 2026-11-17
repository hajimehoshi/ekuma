// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "order"
flags: [noStrict]
---*/

// The decorators are evaluated in source order with the computed keys, and applied
// after all of them, the closest to the element first: the methods, getters, setters
// and accessors, static before instance ones, then the fields, then the class.
var log = [];
function dec(name) {
	log.push("eval " + name);
	return function (value, ctx) {
		log.push(name);
		ctx.addInitializer(function () { log.push("init " + name); });
	};
}
@dec("c1") @dec("c2")
class C {
	@dec("m1") @dec("m2") m() {}
	[(log.push("key"), "k")]() {}
	@dec("f") f = log.push("field f");
	@dec("sf") static sf = log.push("static field sf");
	@dec("a") accessor a;
	@dec("sm") static sm() {}
	static { log.push("static block"); }
}
log.push("new");
new C();
assert.sameValue(String(log.join()), "eval c1,eval c2,eval m1,eval m2,key,eval f,eval sf,eval a,eval sm,sm,m2,m1,a,sf,f,c2,c1,init sm,static field sf,init sf,static block,init c2,init c1,new,init m2,init m1,field f,init f,init a");
