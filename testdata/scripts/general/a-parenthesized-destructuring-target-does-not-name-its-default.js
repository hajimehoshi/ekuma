// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a parenthesized destructuring target does not name its default"
flags: [noStrict]
---*/

var y, z;
({a: (y) = function() {}} = {});
[(z) = function() {}] = [];
assert.sameValue(String(JSON.stringify([y.name, z.name])), "[\"\",\"\"]");
