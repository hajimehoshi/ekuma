// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "index keys and repeated index keys"
flags: [noStrict]
---*/

var o = JSON.parse('{"9999999":1,"1":2,"x":3,"0":4,"9999999":5,"1":6}');
assert.sameValue(String(Object.keys(o).map(function(k) { return k + "=" + o[k]; }).join()), "0=4,1=6,9999999=5,x=3");
