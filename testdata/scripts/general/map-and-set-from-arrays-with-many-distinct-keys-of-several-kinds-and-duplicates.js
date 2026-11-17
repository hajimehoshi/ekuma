// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Map and Set from arrays with many distinct keys of several kinds and duplicates"
flags: [noStrict]
---*/

var o = {}, src = [];
for (var i = 0; i < 16; i++) src.push(i);
for (var i = 0; i < 40; i++) src.push("k" + i % 20, i % 8, o, 10n ** 30n);
var s = new Set(src), m = new Map(src.map((k, i) => [k, i]));
assert.sameValue(String([s.size, m.size, s.has("k19"), s.has(7), s.has(o), s.has(10n ** 30n), m.get("k0"), m.get(o), [...s].slice(14, 19).join()].join(" ")), "38 38 true true true true 96 174 14,15,k0,[object Object],1000000000000000000000000000000");
