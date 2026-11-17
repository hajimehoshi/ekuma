// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a long chain of bound functions"
flags: [noStrict]
---*/

var f = function g() {}, a = f.bind(null), b = a.bind(null), c = a.bind(null);
for (var i = 0; i < 200000; i++) f = f.bind(null);
assert.sameValue(String([f.name.length, f.name.slice(0, 12), f.name.slice(-7), a.name, b.name, c.name].join()), "1200001,bound bound ,bound g,bound g,bound bound g,bound bound g");
