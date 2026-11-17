// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "invalid results and late initializers"
flags: [noStrict]
---*/

var out = [], saved;
for (const [ret, src] of [[1, "m() {}"], [{}, "f"], [1, "accessor a"], [{init: 1}, "accessor a"], [1, ""]]) {
	globalThis.r = () => ret;
	try {
		eval(src ? "(class { @r " + src + " })" : "(@r class {})");
		out.push("none");
	} catch (e) {
		out.push(e.constructor.name);
	}
}
class D { @((v, c) => { saved = c; }) m() {} }
try { saved.addInitializer(() => {}); } catch (e) { out.push(e.constructor.name); }
try { (class { @((v, c) => { c.addInitializer(1); }) m() {} }); } catch (e) { out.push(e.constructor.name); }
assert.sameValue(String(out.join()), "TypeError,TypeError,TypeError,TypeError,TypeError,TypeError,TypeError");
