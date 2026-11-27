// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Function.prototype.apply snapshots dense elements before argument coercions run"
---*/

const args = [{
	toString() {
		args[1] = "changed";
		return "prefix-";
	}
}, "original"];

const result = String.prototype.concat.apply("", args);
assert.sameValue(result, "prefix-original");
assert.sameValue(args[1], "changed");
