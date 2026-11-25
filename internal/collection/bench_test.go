// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func BenchmarkTableSet(b *testing.B) {
	cx, _ := engine.NewExecutionContext(nil)
	for _, kind := range []string{"number", "string", "bigint", "object"} {
		for _, size := range []int{1, 4, 8, 1000, 100000} {
			b.Run(fmt.Sprintf("%s/%d", kind, size), func(b *testing.B) {
				keys := make([]engine.Value, size)
				for i := range keys {
					switch kind {
					case "number":
						keys[i] = storage.Number(float64(i))
					case "string":
						keys[i] = cx.NewString(wtf8.String(strconv.Itoa(i)))
					case "bigint":
						keys[i] = storage.BigIntFromInt64(int64(i))
					case "object":
						keys[i] = storage.ObjectValue(cx.NewObject(nil))
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					var tab collection.Table
					for _, key := range keys {
						tab.Set(key, storage.Undefined)
					}
				}
			})
		}
	}
}
