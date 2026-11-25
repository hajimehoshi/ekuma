// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// These are the embedded types. An embedded type gives the field its name, and the fields must not
// be exported.
type (
	bigIntObject   = storage.BigIntObject
	propertyObject = storage.PropertyObject
	realmPart      = storage.RealmPart
	space          = storage.Space
)
