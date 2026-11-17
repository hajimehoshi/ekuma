// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parent metadata"
flags: [noStrict]
---*/

// The parent metadata is read with Get once all elements are evaluated, before the
// first decorator applies, and it is the prototype only when it is an object.
var log = [];
function dec(name) {
	log.push("eval " + name);
	return (v, c) => { log.push(name + " " + Object.getPrototypeOf(c.metadata)?.p); };
}
class P {
	static get [Symbol.metadata]() { log.push("get " + (this === P)); return { p: 1 }; }
}
@dec("c")
class C extends P {
	@dec("f") f;
	[(log.push("key"), "k")]() {}
	@dec("m") m() {}
}
var out = [log.join()];
for (var parent of [1, undefined, "s", Symbol(), null]) {
	class Q { static [Symbol.metadata] = parent; }
	class R extends Q { @(() => {}) m() {} }
	out.push(Object.getPrototypeOf(R[Symbol.metadata]));
}
@(() => {}) class N extends null {}
out.push(Object.getPrototypeOf(N[Symbol.metadata]));
assert.sameValue(String(out.join(";")), "eval c,eval f,key,eval m,get true,m 1,f 1,c 1;;;;;;");
