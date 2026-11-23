// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A generator preserves a pending return when a nested finally consumes a later return"
---*/

function* nested() {
	try {
		yield 1;
	} finally {
		inner: try {
			yield 2;
		} finally {
			break inner;
		}
	}
}

const it = nested();
it.next();
assert.sameValue(it.return("first").value, 2);
const result = it.return("second");
assert.sameValue(result.value, "first");
assert.sameValue(result.done, true);
