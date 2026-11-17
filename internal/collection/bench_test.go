// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func BenchmarkTableSet(b *testing.B) {
	vm := engine.NewVM(nil)
	for _, kind := range []string{"number", "string", "bigint", "object"} {
		for _, size := range []int{1, 4, 8, 1000, 100000} {
			b.Run(fmt.Sprintf("%s/%d", kind, size), func(b *testing.B) {
				keys := make([]engine.Value, size)
				for i := range keys {
					switch kind {
					case "number":
						keys[i] = engine.Number(float64(i))
					case "string":
						keys[i] = vm.NewString(strconv.Itoa(i))
					case "bigint":
						keys[i] = engine.BigIntFromInt64(int64(i))
					case "object":
						keys[i] = engine.ObjectValue(vm.NewObject(nil))
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					var tab collection.Table
					for _, key := range keys {
						tab.Set(key, engine.Undefined)
					}
				}
			})
		}
	}
}
