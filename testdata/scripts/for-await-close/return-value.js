// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An async generator preserves its return value while awaiting iterator close"
flags: [async]
---*/

async function* values() {
	for await (const value of [1, 2]) yield value;
}

(async () => {
	const it = values();
	await it.next();
	const result = await it.return(42);
	assert.sameValue(result.value, 42);
	assert.sameValue(result.done, true);
})().then(() => $DONE(), $DONE);
