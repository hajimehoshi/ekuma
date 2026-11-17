// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: Reused call frames clear missing parameters and locals across surplus-argument calls
---*/

function read(a, b) {
    var local;
    if (a) local = b;
    return [a, b, local, arguments.length, arguments[2], this.tag].join("|");
}
var first = {read: read, tag: "first"};
var second = {read: read, tag: "second"};
assert.sameValue(first.read(1, 2), "1|2|2|2||first");
assert.sameValue(second.read(0), "0|||1||second");
assert.sameValue(first.read(1, 2, 3), "1|2|2|3|3|first");
assert.sameValue(second.read(0), "0|||1||second");
