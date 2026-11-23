// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An async function resolves its return value in the function's realm"
flags: [async]
---*/

const other = $262.createRealm();
const identity = other.evalScript("(async value => value)");
const {proxy, revoke} = Proxy.revocable({}, {});
revoke();

identity(proxy).then(() => { throw new Test262Error("expected rejection"); }, error => {
	assert.sameValue(Object.getPrototypeOf(error), other.global.TypeError.prototype);
}).then(() => $DONE(), $DONE);
