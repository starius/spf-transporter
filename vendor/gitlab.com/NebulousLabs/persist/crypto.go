package persist

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"golang.org/x/crypto/blake2b"
)

const (
	// HashSize is the length of a Hash in bytes.
	HashSize = 32
)

var (
	// ErrHashWrongLen is the error when encoded value has the wrong
	// length to be a hash.
	ErrHashWrongLen = errors.New("encoded value has the wrong length to be a hash")
)

type (
	// Hash is a BLAKE2b 256-bit digest.
	Hash [HashSize]byte
)

// HashBytes takes a byte slice and returns the result.
func HashBytes(data []byte) Hash {
	return Hash(blake2b.Sum256(data))
}

// MarshalJSON marshales a hash as a hex string.
func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

// String prints the hash in hex.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// UnmarshalJSON decodes the json hex string of the hash.
func (h *Hash) UnmarshalJSON(b []byte) error {
	// *2 because there are 2 hex characters per byte.
	// +2 because the encoded JSON string has a `"` added at the beginning and end.
	if len(b) != HashSize*2+2 {
		return ErrHashWrongLen
	}

	// b[1 : len(b)-1] cuts off the leading and trailing `"` in the JSON string.
	hBytes, err := hex.DecodeString(string(b[1 : len(b)-1]))
	if err != nil {
		return errors.New("could not unmarshal hash: " + err.Error())
	}
	copy(h[:], hBytes)
	return nil
}
