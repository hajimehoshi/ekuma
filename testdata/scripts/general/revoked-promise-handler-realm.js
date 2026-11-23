// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A revoked promise handler throws in the realm that enqueues the reaction"
flags: [async]
---*/

const other = $262.createRealm();
const {proxy, revoke} = Proxy.revocable(() => {}, {});
revoke();

Promise.resolve().then(proxy, other.evalScript("() => {}")).then(
	() => { throw new Test262Error("expected rejection"); },
	error => assert.sameValue(Object.getPrototypeOf(error), TypeError.prototype)
).then(() => $DONE(), $DONE);
