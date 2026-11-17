// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collate

type TailoringEntry = tailoringEntry

func (e *tailoringEntry) Load(compressed string) { e.get(compressed) }
