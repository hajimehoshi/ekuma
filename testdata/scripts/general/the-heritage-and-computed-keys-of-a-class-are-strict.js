// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the heritage and computed keys of a class are strict"
flags: [noStrict]
---*/

var r = [];
try { class B extends (Object.freeze({}).x = 1, Object) {} r.push("no error"); } catch (e) { r.push(e.constructor.name); }
try { class C { [(Object.freeze({}).y = 1, "m")]() {} } r.push("no error"); } catch (e) { r.push(e.constructor.name); }
assert.sameValue(String(r.join()), "TypeError,TypeError");
