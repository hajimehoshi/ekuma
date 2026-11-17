// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "constructor and generator"
flags: [onlyStrict]
---*/

function g() { return 5; }
function F() { this.x = 1; return g(); }
function* gen() { yield 2; }
function h() { return gen(); }
assert.sameValue(String(new F().x + h().next().value), "3");
