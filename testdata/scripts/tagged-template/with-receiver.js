// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An identifier used as a tag receives its with binding object"
flags: [noStrict]
---*/

const object = { tag() { return this; } };
with (object) {
	assert.sameValue(tag``, object);
}
