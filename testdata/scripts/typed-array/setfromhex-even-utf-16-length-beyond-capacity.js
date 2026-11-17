// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "setFromHex even UTF-16 length beyond capacity"
flags: [noStrict]
---*/

var a = new Uint8Array([255]);
var r = a.setFromHex("00éa");
assert.sameValue(String([r.read, r.written, a[0]].join()), "2,1,0");
