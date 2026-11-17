// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "push and unshift through a setter on the prototype of a subclass"
flags: [noStrict]
---*/

var log = [];
class A extends Array {}
Object.defineProperty(A.prototype, 0, { set(v) { log.push(v); }, get() { return "G"; } });
new A().push(1);
new A().unshift(2);
assert.sameValue(String(log.join()), "1,2");
