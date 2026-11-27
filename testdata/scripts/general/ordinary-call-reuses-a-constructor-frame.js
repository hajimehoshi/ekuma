// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An ordinary call reusing a constructor frame sees its own call state"
---*/

function frameState(a, b) {
	if (new.target) {
		this.target = new.target;
		return 1;
	}
	return arguments.length * 10 + a;
}

function callBoth() {
	const constructed = new frameState();
	const result = frameState(2, 3);
	assert.sameValue(constructed.target, frameState);
	assert.sameValue(result, 22);
}

callBoth();
callBoth();
