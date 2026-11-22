// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A cached miss on an ordinary object does not hide a lazy Temporal property"
---*/

function instant(object) {
	return object.Instant;
}

assert.sameValue(instant({}), undefined);
assert.sameValue(typeof instant(Temporal), "function");
