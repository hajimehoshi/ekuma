// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "forwarded arguments with a modified array iterator next"
flags: [noStrict]
---*/

function g(...r) { return r.length; }
(function (a, b) { return g(...arguments); })(1, 2);
Object.getPrototypeOf([][Symbol.iterator]()).next = function () { return { done: true }; };
assert.sameValue(String((function (a, b) { return g(...arguments); })(1, 2)), "0");
