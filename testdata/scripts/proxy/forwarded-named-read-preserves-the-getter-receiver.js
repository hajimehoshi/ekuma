// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A forwarded named read calls an inherited getter with the proxy receiver
---*/

const holder = { get value() { return this; } };
const proxy = new Proxy(Object.create(holder), {});
assert.sameValue(proxy.value, proxy);
