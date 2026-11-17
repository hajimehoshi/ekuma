// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "delete of an optional chain whose last link short-circuits"
flags: [noStrict]
---*/

var o = { a: undefined };
assert.sameValue(String([delete undefined?.x, delete o.a?.["x"], delete o?.a].join()), "true,true,true");
