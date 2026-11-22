// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "built-in constructors take the prototype of new.target"
---*/

assert.sameValue(Object.getPrototypeOf(Reflect.construct(Map, [], Set)), Set.prototype, "another built-in constructor");

var foreign = $262.createRealm().global;
assert.sameValue(Object.getPrototypeOf(Reflect.construct(Map, [], foreign.Map)), foreign.Map.prototype, "a built-in constructor of another realm");

assert.sameValue(Object.getPrototypeOf(Reflect.construct(Map, [], Proxy)), Map.prototype, "a built-in constructor without a prototype property");
