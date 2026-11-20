// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A tagged template keeps the receiver evaluated before its getter runs"
---*/

function check() {
	let object = {
		get tag() {
			object = {};
			return function () { return this; };
		},
	};
	const original = object;
	assert.sameValue(object.tag``, original);
}
check();
