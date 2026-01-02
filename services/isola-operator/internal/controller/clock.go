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
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
}

// RealClock implements Clock using real time functions
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

type FakeClock struct {
	CurrentTime time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{CurrentTime: t}
}

func (f *FakeClock) Now() time.Time {
	return f.CurrentTime
}

func (f *FakeClock) Since(t time.Time) time.Duration {
	return f.CurrentTime.Sub(t)
}

func (f *FakeClock) Until(t time.Time) time.Duration {
	return t.Sub(f.CurrentTime)
}

func (f *FakeClock) Advance(d time.Duration) {
	f.CurrentTime = f.CurrentTime.Add(d)
}

func (f *FakeClock) Set(t time.Time) {
	f.CurrentTime = t
}
