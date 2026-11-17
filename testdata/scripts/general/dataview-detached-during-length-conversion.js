// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "DataView length conversion detaches the buffer"
flags: [noStrict]
negative:
  phase: runtime
  type: TypeError
---*/

var ab = new ArrayBuffer(16);
new DataView(ab, 8, { valueOf() { ab.transfer(); return 8; } });
