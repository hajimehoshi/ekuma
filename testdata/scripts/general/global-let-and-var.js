// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "global let and var"
flags: [noStrict]
---*/

let gl = 1; var gv = 2; function rd() { return gl + gv + (typeof nope); } assert.sameValue(String(rd() + ":" + ("gv" in globalThis) + ("gl" in globalThis)), "3undefined:truefalse");
