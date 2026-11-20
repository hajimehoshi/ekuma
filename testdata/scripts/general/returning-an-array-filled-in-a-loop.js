// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "each collected array has its own elements and sees the current input"
---*/

function collect(entries) {
    var result = [];
    for (var i = 0; i < entries.length; i++) {
        var entry = entries[i];
        if (entry) result.push(entry);
    }
    return result;
}

var first = {}, last = {};
var entries = Array(24).fill(first);
var previous = collect(entries);
collect(entries);
entries.length = 9;
delete entries[1];
entries[8] = last;
var current = collect(entries);
previous.length = 0;
assert.sameValue(current.length, 8);
assert.sameValue(current[0], first);
assert.sameValue(current[7], last);
