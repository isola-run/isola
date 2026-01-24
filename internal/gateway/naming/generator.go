// Package naming provides sandbox name generation utilities.
package naming

import (
	"crypto/rand"
	"math/big"
)

// DNS-safe alphabet: lowercase alphanumeric only (no hyphens to avoid edge cases)
const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// GenerateSandboxName generates a unique, DNS-safe sandbox name.
// Format: 12 random characters from [0-9a-z]
// Entropy: ~62 bits (36^12 ≈ 4.7 × 10^18 combinations)
func GenerateSandboxName() string {
	return generateID(12)
}

// generateID generates a random ID of the specified length using crypto/rand.
func generateID(length int) string {
	result := make([]byte, length)
	alphabetLen := big.NewInt(int64(len(alphabet)))

	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			// Fallback should never happen with crypto/rand, but handle gracefully
			panic("failed to generate random number: " + err.Error())
		}
		result[i] = alphabet[n.Int64()]
	}

	return string(result)
}
