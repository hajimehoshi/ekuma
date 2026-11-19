// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "instanceof finds Symbol.hasInstance on the prototype chain of the constructor"
---*/

var log = [];

class A { static [Symbol.hasInstance](v) { log.push("A"); return v === 1; } }
class B extends A {}
class C extends B {}
assert.sameValue(String([1 instanceof B, new C() instanceof C, 1 instanceof C.bind()]), "true,false,true");
assert.sameValue(log.join(), "A,A,A");

function F() {}
var f = new F();
Object.setPrototypeOf(F, { [Symbol.hasInstance]() { return true; } });
assert.sameValue(String([({}) instanceof F, f instanceof Function.prototype.bind.call(F)]), "true,true");

Object.setPrototypeOf(F, Object.create(Function.prototype));
assert.sameValue(String([({}) instanceof F, f instanceof F]), "false,true");

Object.setPrototypeOf(F, Object.create(Function.prototype, { [Symbol.hasInstance]: { get() { log.push("get"); return function() { return true; }; } } }));
assert.sameValue(String([({}) instanceof F, log.at(-1)]), "true,get");

Object.setPrototypeOf(F, {});
assert.sameValue(String([({}) instanceof F, f instanceof F]), "false,true");

function G() {}
assert.sameValue(({}) instanceof G, false);
function H() {}
Object.defineProperty(H, Symbol.hasInstance, { value: function() { return true; } });
assert.sameValue(({}) instanceof H, true);
