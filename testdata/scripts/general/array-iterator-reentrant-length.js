// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An array iterator saves its object and index before reading length"
---*/

let first = true;
let nested;
const it = Array.prototype.values.call({
    0: "a",
    1: "b",
    get length() {
        if (first) {
            first = false;
            nested = it.next();
        }
        return 2;
    }
});
const outer = it.next();
const following = it.next();
assert.sameValue(nested.value, "a");
assert.sameValue(outer.value, "a");
assert.sameValue(following.value, "b");

const exhausted = Array.prototype.values.call({
    0: "a",
    get length() {
        Object.defineProperty(this, "length", { value: 0 });
        assert.sameValue(exhausted.next().done, true);
        return 1;
    }
});
assert.sameValue(exhausted.next().value, "a");
assert.sameValue(exhausted.next().done, true);
