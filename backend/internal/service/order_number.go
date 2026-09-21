package service

import (
	"crypto/rand"
	"math/big"
	"time"
)

const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func generateOrderNumber(now time.Time) (string, error) {
	var value [16]byte
	milliseconds := uint64(now.UTC().UnixMilli())
	for index := 5; index >= 0; index-- {
		value[index] = byte(milliseconds)
		milliseconds >>= 8
	}
	if _, err := rand.Read(value[6:]); err != nil {
		return "", err
	}

	number := new(big.Int).SetBytes(value[:])
	base := big.NewInt(32)
	remainder := new(big.Int)
	encoded := make([]byte, 26)
	for index := len(encoded) - 1; index >= 0; index-- {
		number.QuoRem(number, base, remainder)
		encoded[index] = crockfordBase32[remainder.Int64()]
	}
	return string(encoded), nil
}
