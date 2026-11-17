// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "fields of a base class are initialized before the parameters"
flags: [noStrict]
---*/

var log = [];
class A { x = log.push("field"); constructor(a = log.push("param")) {} }
new A();
assert.sameValue(String(log.join()), "field,param");
