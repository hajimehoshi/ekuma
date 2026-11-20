// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a read of a global variable that ran before the property was redefined as an accessor or deleted sees the change"
---*/

globalThis.redefined = "data";
function readRedefined() { return redefined; }
assert.sameValue(readRedefined(), "data");
Object.defineProperty(globalThis, "redefined", {
	get() { return "getter"; },
	configurable: true,
});
assert.sameValue(readRedefined(), "getter");

globalThis.deleted = "data";
function readDeleted() { return typeof deleted === "undefined" ? "deleted" : deleted; }
assert.sameValue(readDeleted(), "data");
delete globalThis.deleted;
assert.sameValue(readDeleted(), "deleted");
