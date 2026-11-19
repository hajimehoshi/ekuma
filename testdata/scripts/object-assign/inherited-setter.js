// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.assign calls a setter that the target inherits"
flags: [noStrict]
---*/

const log = [];
Object.defineProperty(Object.prototype, "b", {set(v) { log.push(v); }, configurable: true});
const r = Object.assign({}, {a: 1, b: 2});
assert.sameValue(Object.keys(r).join(), "a");
assert.sameValue(log.join(), "2");
