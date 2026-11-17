// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "access"
flags: [noStrict]
---*/

var acc = {};
function keep(name) { return (v, c) => { acc[name] = c.access; }; }
class P {
	@keep("x") x = 1;
	@keep("y") #y = 2;
	@keep("m") #m() { return this.#y; }
}
var p = new P(), out = [];
out.push(acc.x.get(p), acc.y.get(p), acc.x.has(p), acc.y.has(p), acc.y.has({}), acc.m.get(p).call(p));
acc.x.set(p, 10);
acc.y.set(p, 20);
out.push(p.x, acc.y.get(p), acc.x.get.length, acc.x.set.length, acc.x.has.name);
try { acc.y.get({}); } catch (e) { out.push(e.constructor.name); }
try { acc.x.get(1); } catch (e) { out.push(e.constructor.name); }
assert.sameValue(String(out.join()), "1,2,true,true,false,2,10,20,1,2,has,TypeError,TypeError");
