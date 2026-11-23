// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A cached inherited length on a stripped function does not hide a fresh function's own length"
---*/

function lengthOf(fn) {
	return fn.length;
}

const stripped = () => {};
delete stripped.name;
delete stripped.length;
assert.sameValue(lengthOf(stripped), 0);
assert.sameValue(lengthOf((x, y) => {}), 2);
