package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Till PINs are hashed as the till hashes them (apps/pos/store_staff.go),
// so a hash made here checks on the till and one made there checks here:
// "argon2id$<salt>$<hash>", Argon2id with 1 pass and 8 MiB. Four digits
// make 10,000 PINs, which no hash keeps from a stolen database; the costs
// are kept small so an old till checks every person's PIN quickly. What
// protects PINs is that they are only for signing in to a till in the shop.
const (
	pinTime   = 1
	pinMemory = 8 * 1024
	pinKeyLen = 32
	PINLength = 4
)

// ValidPIN reports whether pin is 4 digits.
func ValidPIN(pin string) bool {
	if len(pin) != PINLength {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// HashPIN hashes a till PIN.
func HashPIN(pin string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey([]byte(pin), salt, pinTime, pinMemory, 1, pinKeyLen)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(sum)
}

// CheckPIN reports whether pin matches a till PIN hash.
func CheckPIN(hash, pin string) bool {
	salt, want, ok := splitPINHash(hash)
	if !ok {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, pinTime, pinMemory, 1, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// WellFormedPINHash reports whether a hash a till sends has the shape of
// one.
func WellFormedPINHash(hash string) bool {
	_, want, ok := splitPINHash(hash)
	return ok && len(want) == pinKeyLen
}

func splitPINHash(hash string) (salt, sum []byte, ok bool) {
	parts := strings.Split(hash, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return nil, nil, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	sum, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	return salt, sum, err1 == nil && err2 == nil && len(salt) >= 8
}
