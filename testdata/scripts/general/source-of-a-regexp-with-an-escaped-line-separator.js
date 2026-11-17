// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "source of a RegExp with an escaped line separator"
flags: [noStrict]
---*/

var r = RegExp("\\\u2028");
assert.sameValue(String([r.source, RegExp("\\\u2029").source, eval("/" + r.source + "/").test("\u2028")].join()), "\\u2028,\\u2029,true");
