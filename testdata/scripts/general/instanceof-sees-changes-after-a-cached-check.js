// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "instanceof sees changes made after a cached check"
---*/

function check(v, F) { return v instanceof F; }
function warm(v, F) { check(v, F); check(v, F); return check(v, F); }

function C() {}
var c = new C();
assert.sameValue(warm(c, C), true);
C.prototype = {};
assert.sameValue(check(c, C), false, "a new prototype object");

function D() {}
var d = new D();
assert.sameValue(warm(d, D), true);
Object.defineProperty(D, Symbol.hasInstance, { value: function () { return false; } });
assert.sameValue(check(d, D), false, "an own @@hasInstance");

function PrivateShape() {}
PrivateShape.spare = 1;
delete PrivateShape.spare;
var x = new PrivateShape();
assert.sameValue(check(x, PrivateShape), true);
Object.defineProperty(PrivateShape, Symbol.hasInstance, { value: function () { return false; } });
assert.sameValue(check(x, PrivateShape), false, "an own @@hasInstance after deleting a property");

class A {}
class B extends A {}
var b = new B();
assert.sameValue(warm(b, B), true);
Object.defineProperty(A, Symbol.hasInstance, { value: function () { return false; } });
assert.sameValue(check(b, B), false, "an inherited @@hasInstance");

function E() {}
var trapped = 0;
var p = new Proxy({}, { getPrototypeOf: function () { trapped++; return E.prototype; } });
assert.sameValue(warm(new E(), E), true);
assert.sameValue(check(p, E), true, "a proxy on the chain");
assert.sameValue(trapped, 1, "the getPrototypeOf trap runs");

function F() {}
assert.sameValue(warm(new F(), F), true);
F.prototype = 1;
var caught;
try { check({}, F); } catch (e) { caught = e; }
assert.sameValue(caught instanceof TypeError, true, "a primitive prototype");
