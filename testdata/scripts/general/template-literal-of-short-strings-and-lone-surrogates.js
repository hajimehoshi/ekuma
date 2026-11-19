// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a template literal joins short strings and lone surrogates"
flags: [noStrict]
---*/

function combine(a, b, c) { return `${a}${b}${c}`; }
const hi = String.fromCharCode(0xd83d);
const lo = String.fromCharCode(0xde00);
assert.sameValue(combine(hi, "", lo), "😀");
assert.sameValue(combine(String(12), "", String(34)), "1234");
assert.sameValue(combine("long-prefix", String(12345678), hi), "long-prefix12345678\ud83d");
