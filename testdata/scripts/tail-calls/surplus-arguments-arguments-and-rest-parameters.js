// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "surplus arguments, arguments and rest parameters"
flags: [onlyStrict]
---*/

function f(n) { return n === 0 ? arguments.length : g(n, 1, 2, 3); }
function g(n, ...rest) { return f(n - 1, ...rest, rest.length); }
assert.sameValue(String(f(1e5)), "5");
