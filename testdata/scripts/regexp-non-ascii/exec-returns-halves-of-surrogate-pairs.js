// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exec without the u flag returns captures that start or end inside a surrogate pair"
---*/

// Long enough not to be packed into a value.
var t = "abcdefghij😀klmnop";
var m = /\uD83D/.exec(t);
assert.sameValue(m[0], "\uD83D");
assert.sameValue(m.index, 10);

m = /\uDE00k/.exec(t);
assert.sameValue(m[0], "\uDE00k");
assert.sameValue(m.index, 11);

m = /(j\uD83D)(\uDE00)(kl)/.exec(t);
assert.sameValue([m[1] === "j\uD83D", m[2] === "\uDE00", m[3], m.index].join("|"), "true|true|kl|9");

m = /(😀)/.exec(t);
assert.sameValue(m[1], "😀");

// Short enough to be packed.
var s = "a😀b";
m = /(\uD83D)(\uDE00b)/.exec(s);
assert.sameValue([m[1] === "\uD83D", m[2] === "\uDE00b", m.index].join("|"), "true|true|1");
