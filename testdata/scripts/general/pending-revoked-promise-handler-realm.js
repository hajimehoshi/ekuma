// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A pending reaction whose handler is revoked uses the settling resolver's realm"
flags: [async]
---*/

const handlerRealm = $262.createRealm();
const resolverRealm = $262.createRealm();
const {proxy, revoke} = handlerRealm.evalScript("Proxy.revocable(() => {}, {})");

// Borrow the foreign Promise constructor to get its resolver while keeping
// the local Promise.prototype.then and species constructor.
let resolve;
const promise = Reflect.construct(resolverRealm.global.Promise, [r => { resolve = r; }], Promise);
const result = promise.then(proxy);

revoke();
resolve();

result.then(
	() => { throw new Test262Error("expected rejection"); },
	error => assert.sameValue(Object.getPrototypeOf(error), resolverRealm.global.TypeError.prototype)
).then(() => $DONE(), $DONE);
