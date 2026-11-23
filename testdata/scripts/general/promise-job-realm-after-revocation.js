// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Bound Promise callbacks revoked after enqueueing throw in the saved target realm"
flags: [async]
---*/

const other = $262.createRealm();
const {proxy, revoke} = Proxy.revocable(other.evalScript("() => {}"), {});
const callback = proxy.bind(null);
const reaction = Promise.reject(1).catch(callback);
const thenable = Promise.resolve({then: callback});
revoke();

function checkRejection(promise) {
	return promise.then(
		() => { throw new Test262Error("expected rejection"); },
		error => assert.sameValue(Object.getPrototypeOf(error), other.global.TypeError.prototype)
	);
}

Promise.all([checkRejection(reaction), checkRejection(thenable)]).then(() => $DONE(), $DONE);
