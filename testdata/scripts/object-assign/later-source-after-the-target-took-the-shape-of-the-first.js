// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a later source of Object.assign changes a target that took the shape of the first source, and leaves the first source alone"
flags: [noStrict]
---*/

const first = {a: 1, b: 2};
const r = Object.assign({}, first, {b: 3, c: 4});
assert.sameValue(JSON.stringify(r), '{"a":1,"b":3,"c":4}');
assert.sameValue(JSON.stringify(first), '{"a":1,"b":2}');
