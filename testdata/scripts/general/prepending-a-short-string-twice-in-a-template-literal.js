// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a template literal puts a short string twice before a prepended string"
flags: [noStrict]
---*/

let tail = "";
for (let i = 0; i < 64; i++) tail = "z" + tail;
const prefix = String(12);
const result = `${prefix}${prefix}${tail}`;
assert.sameValue(result, "1212" + "z".repeat(64));
