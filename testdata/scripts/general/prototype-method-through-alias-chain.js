// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prototype method through alias chain"
flags: [noStrict]
---*/

function A() { this.x = 1; }
A.prototype.get = function(d) { return this.x + d; };
var alias = A.prototype.get;
A.prototype.get = function() { return alias.apply(this, arguments) * 2; };
assert.sameValue(String(new A().get(4)), "10");
