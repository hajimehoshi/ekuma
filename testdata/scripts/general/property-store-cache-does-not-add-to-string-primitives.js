// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a property-store cache does not add properties to string primitives"
flags: [noStrict]
---*/

function sloppyStore(value) {
    value.p = 1;
}
function strictStore(value) {
    "use strict";
    value.p = 1;
}

var string = ["ab", "cd"].join("");
sloppyStore(Object.create(String.prototype));
sloppyStore(string);
assert.sameValue(string.p, undefined);

strictStore(Object.create(String.prototype));
var caught;
try {
    strictStore(string);
} catch (error) {
    caught = error;
}
assert.sameValue(caught instanceof TypeError, true);
