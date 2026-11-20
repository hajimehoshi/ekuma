// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "String.raw joins a surrogate pair split between two of many parts"
flags: [noStrict]
---*/

var raw = [];
for (var i = 0; i < 15; i++) raw.push("a");
raw.push("\uD83D", "\uDE00", "b");
assert.sameValue(String.raw({raw: raw}), "aaaaaaaaaaaaaaa\u{1F600}b");
