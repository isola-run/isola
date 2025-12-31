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

import "time"

// Clock interface allows mocking time in tests for deterministic behavior
type Clock interface {
	// Now returns the current time
	Now() time.Time
	// Since returns the time elapsed since t
	Since(t time.Time) time.Duration
	// Until returns the duration until t
	Until(t time.Time) time.Duration
}

// RealClock implements Clock using real time functions
type RealClock struct{}

// Now returns the current real time
func (RealClock) Now() time.Time {
	return time.Now()
}

// Since returns the time elapsed since t
func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Until returns the duration until t
func (RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

// FakeClock implements Clock with controllable time for testing
type FakeClock struct {
	CurrentTime time.Time
}

// NewFakeClock creates a FakeClock set to the given time
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{CurrentTime: t}
}

// Now returns the fake current time
func (f *FakeClock) Now() time.Time {
	return f.CurrentTime
}

// Since returns the duration since t based on the fake current time
func (f *FakeClock) Since(t time.Time) time.Duration {
	return f.CurrentTime.Sub(t)
}

// Until returns the duration until t based on the fake current time
func (f *FakeClock) Until(t time.Time) time.Duration {
	return t.Sub(f.CurrentTime)
}

// Advance moves the fake clock forward by the given duration
func (f *FakeClock) Advance(d time.Duration) {
	f.CurrentTime = f.CurrentTime.Add(d)
}

// Set sets the fake clock to a specific time
func (f *FakeClock) Set(t time.Time) {
	f.CurrentTime = t
}
