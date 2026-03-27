// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package env

import (
	"math"
	"strconv"
	"testing"
)

func TestGetOrDefault(t *testing.T) {
	const key = "TEST_GET_OR_DEFAULT"

	t.Run("returns env var when set", func(t *testing.T) {
		t.Setenv(key, "custom-value")
		if got := GetOrDefault(key, "fallback"); got != "custom-value" {
			t.Errorf("got %q, want %q", got, "custom-value")
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		if got := GetOrDefault(key, "fallback"); got != "fallback" {
			t.Errorf("got %q, want %q", got, "fallback")
		}
	})

	t.Run("returns default when set to empty string", func(t *testing.T) {
		t.Setenv(key, "")
		if got := GetOrDefault(key, "fallback"); got != "fallback" {
			t.Errorf("got %q, want %q", got, "fallback")
		}
	})
}

func TestGetOrDefaultInt(t *testing.T) {
	const key = "TEST_GET_OR_DEFAULT_INT"

	t.Run("returns parsed int when valid", func(t *testing.T) {
		t.Setenv(key, "42")
		if got := GetOrDefaultInt(key, 10); got != 42 {
			t.Errorf("got %d, want %d", got, 42)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		if got := GetOrDefaultInt(key, 10); got != 10 {
			t.Errorf("got %d, want %d", got, 10)
		}
	})

	t.Run("returns default when set to empty string", func(t *testing.T) {
		t.Setenv(key, "")
		if got := GetOrDefaultInt(key, 10); got != 10 {
			t.Errorf("got %d, want %d", got, 10)
		}
	})

	t.Run("returns default for non-numeric string", func(t *testing.T) {
		t.Setenv(key, "not-a-number")
		if got := GetOrDefaultInt(key, 10); got != 10 {
			t.Errorf("got %d, want %d", got, 10)
		}
	})

	t.Run("handles negative numbers", func(t *testing.T) {
		t.Setenv(key, "-5")
		if got := GetOrDefaultInt(key, 10); got != -5 {
			t.Errorf("got %d, want %d", got, -5)
		}
	})

	t.Run("handles zero", func(t *testing.T) {
		t.Setenv(key, "0")
		if got := GetOrDefaultInt(key, 10); got != 0 {
			t.Errorf("got %d, want %d", got, 0)
		}
	})

	t.Run("returns default for very large number exceeding int range", func(t *testing.T) {
		overflow := strconv.FormatInt(math.MaxInt64, 10) + "0"
		t.Setenv(key, overflow)
		if got := GetOrDefaultInt(key, 10); got != 10 {
			t.Errorf("got %d, want %d", got, 10)
		}
	})
}
