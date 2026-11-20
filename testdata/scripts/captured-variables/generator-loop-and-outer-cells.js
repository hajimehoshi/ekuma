// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "generator closures keep distinct loop cells and share the outer cell"
---*/

function* counters() {
    let outer = 0;
    for (let i = 0; i < 2; i++) {
        yield () => [++i, ++outer];
    }
}

const [first, second] = [...counters()];
assert.sameValue(first().join(","), "1,1");
assert.sameValue(second().join(","), "2,2");
