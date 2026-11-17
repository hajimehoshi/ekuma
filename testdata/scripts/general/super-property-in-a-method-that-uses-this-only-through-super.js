// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "super property in a method that uses this only through super"
flags: [noStrict]
---*/

class A { m() { return this; } set x(v) { this.y = v; } }
class B extends A { m() { return super.m(); } setX() { super.x = 1; } }
var b = new B(); b.setX(); assert.sameValue(String((b.m() === b) + ":" + b.y), "true:1");
