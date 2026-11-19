// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a template literal puts a lead surrogate twice before a prepended string that starts with a trail surrogate"
flags: [noStrict]
---*/

let tail = "";
for (let i = 0; i < 64; i++) tail = "z" + tail;
const prefix = String.fromCharCode(0xd83d);
tail = String.fromCharCode(0xde00) + tail;
const result = `${prefix}${prefix}${tail}`;
assert.sameValue(result, "\ud83d😀" + "z".repeat(64));
