// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assigning RegExp.input after a subclass match restores only the input slot"
flags: [noStrict]
---*/

function assertUnavailable(name) {
    let caught;
    try {
        RegExp[name];
    } catch (e) {
        caught = e;
    }
    assert.sameValue(caught instanceof TypeError, true);
}

class Derived extends RegExp {}
new Derived("b").exec("abc");
assertUnavailable("input");

RegExp.input = "manual";
assert.sameValue(RegExp.input, "manual");
assert.sameValue(RegExp.$_, "manual");
RegExp.$_ = 42;
assert.sameValue(RegExp.input, "42");
assertUnavailable("lastMatch");

/c/.exec("abc");
assert.sameValue(RegExp.lastMatch, "c");
assert.sameValue(RegExp.input, "abc");
