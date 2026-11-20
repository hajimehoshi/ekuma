// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the Kelvin sign is invalid in the collation option"
negative:
  phase: runtime
  type: RangeError
---*/

new Intl.Collator("de", {collation: "phonebK"});
