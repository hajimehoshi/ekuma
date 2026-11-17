// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "setFromHex odd UTF-16 length before writing"
flags: [noStrict]
---*/

var a = new Uint8Array([255]), syntaxError = false;
try { a.setFromHex("00é"); } catch (e) { syntaxError = e instanceof SyntaxError; }
assert.sameValue(String([syntaxError, a[0]].join()), "true,255");
