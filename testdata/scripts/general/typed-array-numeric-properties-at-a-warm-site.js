// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A cached property read distinguishes typed arrays from ordinary objects with the same prototype"
---*/

function read(object) {
	return object["-1"];
}

function check(view) {
	const proto = Object.getPrototypeOf(view);
	proto["-1"] = 42;
	assert.sameValue(read(Object.create(proto)), 42);
	assert.sameValue(read(view), undefined);
}

const view = new Uint8Array(1);
check(view);
Object.setPrototypeOf(view, {});
check(view);
