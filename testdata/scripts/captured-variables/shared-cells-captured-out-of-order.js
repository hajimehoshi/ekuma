// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "closures share cells captured out of stack-slot order after the frame returns"
---*/

function capture(a, b, c, d, e, f, g, h, i, j) {
    const write = () => { ++a; ++c; ++d; ++e; ++f; ++g; ++h; ++i; ++j; };
    const writeB = () => ++b;
    const read = () => [a, b, c, d, e, f, g, h, i, j];
    return [write, writeB, read];
}

const [write, writeB, read] = capture(0, 1, 2, 3, 4, 5, 6, 7, 8, 9);
write();
writeB();
assert.sameValue(read().join(","), "1,2,3,4,5,6,7,8,9,10");
