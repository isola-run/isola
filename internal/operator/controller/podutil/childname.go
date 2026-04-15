/*
Copyright 2018 The Knative Authors

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

// Adapted from https://github.com/knative/pkg/blob/main/kmeta/names.go
//
// Modifications from the upstream version:
//   - makeValidName is invoked on every return path, defending against
//     trailing non-alphanumeric characters in the parent (knative/pkg#2659)
//     even when the suffix would otherwise mask them.

package podutil

import (
	"crypto/md5" // #nosec G401 -- used for name uniqueness, not security
	"encoding/hex"
	"fmt"
	"regexp"
)

const (
	// longest is the maximum length of a DNS-1123 label, which is the
	// strictest name limit applied by Kubernetes (Services, the job-name
	// label, etc.). Capping at this size keeps generated names usable
	// everywhere a parent name might be referenced.
	longest = 63
	md5Len  = 32
	// head is how much of the parent we can keep before the MD5 hex hash
	// fills the remainder of the budget.
	head = longest - md5Len
)

var isAlphanumeric = regexp.MustCompile("^[a-zA-Z0-9]$")

// ChildName generates a name for a child resource derived from `parent` plus
// `suffix`. When the simple concatenation would exceed Kubernetes' 63-char
// DNS-1123 label limit, the parent is truncated and the MD5 hex of the
// original parent is spliced in so distinct long parents still yield distinct
// child names.
//
// For short parents (the common case in this operator, where parents are
// 22-char nanoid sandbox / snapshot IDs) the result is the unmodified
// `parent + suffix` concatenation.
func ChildName(parent, suffix string) string {
	n := parent
	if len(parent) > (longest - len(suffix)) {
		// Suffix alone is so long that there's no room to keep parent prefix
		// plus a full hash; hash the (parent+suffix) pair, keep as much
		// parent prefix as fits, then append as much suffix as still fits.
		if head-len(suffix) <= 0 {
			h := md5.Sum([]byte(parent + suffix)) // #nosec G401
			if head < len(parent) {
				parent = parent[:head]
			}
			ret := parent + hex.EncodeToString(h[:])
			if d := longest - len(ret); d > 0 {
				ret += suffix[:d]
			}
			return makeValidName(ret)
		}
		n = fmt.Sprintf("%s%x", parent[:head-len(suffix)], md5.Sum([]byte(parent))) // #nosec G401
	}
	return makeValidName(n + suffix)
}

// makeValidName strips trailing characters that aren't allowed at the end of
// a DNS-1123 name (which must end in [a-z0-9]).
func makeValidName(n string) string {
	for len(n) > 0 && !isAlphanumeric.MatchString(n[len(n)-1:]) {
		n = n[:len(n)-1]
	}
	return n
}
