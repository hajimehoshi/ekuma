// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "sort deletes every index after the sorted values"
flags: [noStrict]
---*/

var deleted = [];
var p = new Proxy([, , 0], { deleteProperty(t, k) { deleted.push(k); return delete t[k]; } });
Array.prototype.sort.call(p);
assert.sameValue(String(deleted.join()), "1,2");
