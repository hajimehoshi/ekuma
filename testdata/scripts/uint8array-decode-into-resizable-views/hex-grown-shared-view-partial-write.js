// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "hex grown shared view partial write"
flags: [noStrict]
---*/

const buffer = new SharedArrayBuffer(2, {maxByteLength: 8});
const dst = new Uint8Array(buffer, 1);
buffer.grow(4);
let syntaxError = false;
try { dst.setFromHex("0102gg"); } catch (e) { syntaxError = e instanceof SyntaxError; }
assert.sameValue(String([syntaxError, new Uint8Array(buffer).join()].join(";")), "true;0,1,2,0");
