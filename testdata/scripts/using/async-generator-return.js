// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Async generator return awaits disposal, including disposal that rejects"
flags: [noStrict, async]
---*/

var log = [];
async function* g(error) {
	await using a = {
		async [Symbol.asyncDispose]() { await null; log.push("disposed"); if (error) throw error; },
	};
	yield;
}
var it = g(), it2 = g("boom");
var first = it.next().then(() => it.return(1)).then(r => log.push(JSON.stringify(r)));
var second = it2.next().then(() => it2.return(2)).catch(e => log.push(e));
Promise.all([first, second]).then(() => {
	assert.sameValue(log.join(), 'disposed,disposed,{"value":1,"done":true},boom');
}).then(() => $DONE(), $DONE);
