// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A promise reaction that returns its own result rejects with a TypeError from the resolver's realm"
flags: [async]
---*/

const other = $262.createRealm();
const handler = other.evalScript("() => result");
const result = Promise.resolve().then(handler);
other.global.result = result;

result.then(() => { throw new Test262Error("expected rejection"); }, error => {
	assert.sameValue(Object.getPrototypeOf(error), TypeError.prototype);
}).then(() => $DONE(), $DONE);
