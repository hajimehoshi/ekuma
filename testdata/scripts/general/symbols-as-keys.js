// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "symbols as keys"
flags: [noStrict]
---*/

var s = Symbol("k"), o = { [s]: 1, x: 2 }; assert.sameValue(String([o[s], Object.keys(o).length, typeof s, s.toString(), Object.getOwnPropertySymbols(o).length].join()), "1,1,symbol,Symbol(k),1");
