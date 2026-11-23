// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A resolving function created by a foreign thenable job rejects in its own realm"
flags: [async]
---*/

const other = $262.createRealm();
const promise = Promise.resolve({
	then: other.evalScript("resolve => { globalThis.resolve = resolve; }")
});
Promise.resolve().then(() => other.global.resolve(promise));

promise.then(() => { throw new Test262Error("expected rejection"); }, error => {
	assert.sameValue(Object.getPrototypeOf(error), other.global.TypeError.prototype);
}).then(() => $DONE(), $DONE);
