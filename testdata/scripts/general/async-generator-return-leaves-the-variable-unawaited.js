// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "return in an async generator leaves the variable unawaited"
flags: [noStrict, async]
---*/

var seen;
async function* g() {
	let x = Promise.resolve(1);
	try {
		return x;
	} finally {
		seen = x instanceof Promise;
	}
}
g().next().then(r => {
	assert.sameValue(r.value, 1);
	assert.sameValue(seen, true);
}).then(() => $DONE(), $DONE);
