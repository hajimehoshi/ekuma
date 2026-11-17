// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Symbol.metadata"
flags: [noStrict]
---*/

var d = Object.getOwnPropertyDescriptor(Symbol, "metadata");
var f = Object.getOwnPropertyDescriptor(Function.prototype, Symbol.metadata);
assert.sameValue(String([typeof Symbol.metadata, Symbol.metadata.description, d.writable, d.enumerable, d.configurable,
	f.value, f.writable, f.enumerable, f.configurable, (function () {})[Symbol.metadata]].join()), "symbol,Symbol.metadata,false,false,false,,false,false,false,");
