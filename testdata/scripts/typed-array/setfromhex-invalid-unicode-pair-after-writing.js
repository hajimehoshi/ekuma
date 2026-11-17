// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "setFromHex invalid Unicode pair after writing"
flags: [noStrict]
---*/

var a = new Uint8Array([255, 255]), syntaxError = false;
try { a.setFromHex("00éa"); } catch (e) { syntaxError = e instanceof SyntaxError; }
assert.sameValue(String([syntaxError, ...a].join()), "true,0,255");
