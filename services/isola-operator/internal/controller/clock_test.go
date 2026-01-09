/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Compile-time check that RealClock satisfies Clock interface
var _ Clock = (*RealClock)(nil)

// Compile-time check that FakeClock satisfies Clock interface
var _ Clock = (*FakeClock)(nil)

func TestNewFakeClock(t *testing.T) {
	baseTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	clock := NewFakeClock(baseTime)

	assert.NotNil(t, clock)
	assert.Equal(t, baseTime, clock.CurrentTime)
}

func TestFakeClock_Now(t *testing.T) {
	tests := []struct {
		name        string
		currentTime time.Time
	}{
		{
			name:        "returns current time - UTC",
			currentTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "returns current time - with nanoseconds",
			currentTime: time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC),
		},
		{
			name:        "returns current time - zero time",
			currentTime: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{CurrentTime: tt.currentTime}

			result := clock.Now()

			assert.Equal(t, tt.currentTime, result)
		})
	}
}

func TestFakeClock_Since(t *testing.T) {
	tests := []struct {
		name         string
		currentTime  time.Time
		pastTime     time.Time
		expectedDiff time.Duration
	}{
		{
			name:         "5 seconds elapsed",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 5, 0, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: 5 * time.Second,
		},
		{
			name:         "1 hour elapsed",
			currentTime:  time.Date(2025, 1, 15, 11, 30, 0, 0, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: 1 * time.Hour,
		},
		{
			name:         "1 day elapsed",
			currentTime:  time.Date(2025, 1, 16, 10, 30, 0, 0, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: 24 * time.Hour,
		},
		{
			name:         "negative duration when time is in future",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 5, 0, time.UTC),
			expectedDiff: -5 * time.Second,
		},
		{
			name:         "zero duration when times are equal",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: 0,
		},
		{
			name:         "nanosecond precision",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC),
			pastTime:     time.Date(2025, 1, 15, 10, 30, 0, 123, time.UTC),
			expectedDiff: 123456666 * time.Nanosecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{CurrentTime: tt.currentTime}

			result := clock.Since(tt.pastTime)

			assert.Equal(t, tt.expectedDiff, result)
		})
	}
}

func TestFakeClock_Until(t *testing.T) {
	tests := []struct {
		name         string
		currentTime  time.Time
		futureTime   time.Time
		expectedDiff time.Duration
	}{
		{
			name:         "5 seconds until future",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			futureTime:   time.Date(2025, 1, 15, 10, 30, 5, 0, time.UTC),
			expectedDiff: 5 * time.Second,
		},
		{
			name:         "1 hour until future",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			futureTime:   time.Date(2025, 1, 15, 11, 30, 0, 0, time.UTC),
			expectedDiff: 1 * time.Hour,
		},
		{
			name:         "1 day until future",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			futureTime:   time.Date(2025, 1, 16, 10, 30, 0, 0, time.UTC),
			expectedDiff: 24 * time.Hour,
		},
		{
			name:         "negative duration when time is in past",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 5, 0, time.UTC),
			futureTime:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: -5 * time.Second,
		},
		{
			name:         "zero duration when times are equal",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			futureTime:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDiff: 0,
		},
		{
			name:         "nanosecond precision",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 123, time.UTC),
			futureTime:   time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC),
			expectedDiff: 123456666 * time.Nanosecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{CurrentTime: tt.currentTime}

			result := clock.Until(tt.futureTime)

			assert.Equal(t, tt.expectedDiff, result)
		})
	}
}

func TestFakeClock_Advance(t *testing.T) {
	tests := []struct {
		name         string
		currentTime  time.Time
		advance      time.Duration
		expectedTime time.Time
	}{
		{
			name:         "advance by 5 seconds",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			advance:      5 * time.Second,
			expectedTime: time.Date(2025, 1, 15, 10, 30, 5, 0, time.UTC),
		},
		{
			name:         "advance by 1 hour",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			advance:      1 * time.Hour,
			expectedTime: time.Date(2025, 1, 15, 11, 30, 0, 0, time.UTC),
		},
		{
			name:         "advance by 1 day",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			advance:      24 * time.Hour,
			expectedTime: time.Date(2025, 1, 16, 10, 30, 0, 0, time.UTC),
		},
		{
			name:         "advance by negative duration (go back)",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			advance:      -5 * time.Second,
			expectedTime: time.Date(2025, 1, 15, 10, 29, 55, 0, time.UTC),
		},
		{
			name:         "advance by zero",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			advance:      0,
			expectedTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:         "advance with nanosecond precision",
			currentTime:  time.Date(2025, 1, 15, 10, 30, 0, 123, time.UTC),
			advance:      123456666 * time.Nanosecond,
			expectedTime: time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{CurrentTime: tt.currentTime}

			clock.Advance(tt.advance)

			assert.Equal(t, tt.expectedTime, clock.CurrentTime)
		})
	}
}

func TestFakeClock_Set(t *testing.T) {
	tests := []struct {
		name        string
		initialTime time.Time
		newTime     time.Time
	}{
		{
			name:        "set to future time",
			initialTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			newTime:     time.Date(2025, 1, 20, 15, 45, 30, 0, time.UTC),
		},
		{
			name:        "set to past time",
			initialTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			newTime:     time.Date(2025, 1, 10, 5, 15, 20, 0, time.UTC),
		},
		{
			name:        "set to same time",
			initialTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			newTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "set to zero time",
			initialTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			newTime:     time.Time{},
		},
		{
			name:        "set from zero time",
			initialTime: time.Time{},
			newTime:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "set with nanosecond precision",
			initialTime: time.Date(2025, 1, 15, 10, 30, 0, 123, time.UTC),
			newTime:     time.Date(2025, 1, 15, 10, 30, 0, 987654321, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{CurrentTime: tt.initialTime}

			clock.Set(tt.newTime)

			assert.Equal(t, tt.newTime, clock.CurrentTime)
		})
	}
}

func TestRealClock_Now(t *testing.T) {
	clock := RealClock{}

	before := time.Now()
	result := clock.Now()
	after := time.Now()

	assert.True(t, result.After(before) || result.Equal(before),
		"clock.Now() should be after or equal to time before call")
	assert.True(t, result.Before(after) || result.Equal(after),
		"clock.Now() should be before or equal to time after call")
	assert.WithinDuration(t, time.Now(), result, 1*time.Second,
		"clock.Now() should be within 1 second of actual current time")
}

func TestRealClock_Since(t *testing.T) {
	clock := RealClock{}

	pastTime := time.Now().Add(-5 * time.Second)
	result := clock.Since(pastTime)

	assert.True(t, result >= 5*time.Second,
		"Since should return at least 5 seconds for a time 5 seconds ago")
	assert.True(t, result < 6*time.Second,
		"Since should return less than 6 seconds for a time 5 seconds ago")
}

func TestRealClock_Until(t *testing.T) {
	clock := RealClock{}

	futureTime := time.Now().Add(5 * time.Second)
	result := clock.Until(futureTime)

	assert.True(t, result > 4*time.Second,
		"Until should return more than 4 seconds for a time 5 seconds in future")
	assert.True(t, result <= 5*time.Second,
		"Until should return at most 5 seconds for a time 5 seconds in future")
}

func TestFakeClock_MultipleOperations(t *testing.T) {
	baseTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := NewFakeClock(baseTime)

	assert.Equal(t, baseTime, clock.Now())

	clock.Advance(5 * time.Minute)
	expectedTime := baseTime.Add(5 * time.Minute)
	assert.Equal(t, expectedTime, clock.Now())
	assert.Equal(t, 5*time.Minute, clock.Since(baseTime))

	newTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	clock.Set(newTime)
	assert.Equal(t, newTime, clock.Now())

	futureTime := newTime.Add(1 * time.Hour)
	assert.Equal(t, 1*time.Hour, clock.Until(futureTime))
}
