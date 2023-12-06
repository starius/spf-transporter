package crypto

// hash.go supplies a few geneeral hashing functions, using the hashing
// algorithm blake2b. Because changing the hashing algorithm for ScPrime has much
// stronger implications than changing any of the other algorithms, blake2b is
// the only supported algorithm. ScPrime is not really flexible enough to support
// multiple.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"

	"gitlab.com/NebulousLabs/encoding"
	"golang.org/x/crypto/blake2b"
)

const (
	// HashSize is the length of a Hash in bytes.
	HashSize = 32
)

type (
	// Hash is a BLAKE2b 256-bit digest.
	Hash [HashSize]byte

	// HashSlice is used for sorting
	HashSlice []Hash
)

var (
	// ErrHashWrongLen is the error when encoded value has the wrong
	// length to be a hash.
	ErrHashWrongLen = errors.New("encoded value has the wrong length to be a hash")
)

// NewHash returns a blake2b 256bit hasher.
func NewHash() hash.Hash {
	h, _ := blake2b.New256(nil) // cannot fail with nil argument
	return h
}

// HashAll takes a set of objects as input, encodes them all using the encoding
// package, and then hashes the result.
func HashAll(objs ...interface{}) (hash Hash) {
	h := NewHash()
	enc := encoding.NewEncoder(h)
	for _, obj := range objs {
		enc.Encode(obj)
	}
	h.Sum(hash[:0])
	return
}

// HashBytes takes a byte slice and returns the result.
func HashBytes(data []byte) Hash {
	return Hash(blake2b.Sum256(data))
}

// HashObject takes an object as input, encodes it using the encoding package,
// and then hashes the result.
func HashObject(obj interface{}) (hash Hash) {
	h := NewHash()
	encoding.NewEncoder(h).Encode(obj)
	h.Sum(hash[:0])
	return
}

// These functions implement sort.Interface, allowing hashes to be sorted.
func (hs HashSlice) Len() int           { return len(hs) }
func (hs HashSlice) Less(i, j int) bool { return bytes.Compare(hs[i][:], hs[j][:]) < 0 }
func (hs HashSlice) Swap(i, j int)      { hs[i], hs[j] = hs[j], hs[i] }

// String prints the hash in hex.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// LoadString takes a string, parses the hash value of the string, and sets the
// value of the hash equal to the hash value of the string.
func (h *Hash) LoadString(s string) error {
	// *2 because there are 2 hex characters per byte.
	if len(s) != HashSize*2 {
		return ErrHashWrongLen
	}
	hBytes, err := hex.DecodeString(s)
	if err != nil {
		return errors.New("could not unmarshal hash: " + err.Error())
	}
	copy(h[:], hBytes)
	return nil
}

// MarshalText returns the hex string of the hash.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

// UnmarshalText decodes the hex string of the hash.
func (h *Hash) UnmarshalText(text []byte) error {
	return h.LoadString(string(text))
}

// MarshalJSON marshals a hash as a hex string.
func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

// UnmarshalJSON decodes the json hex string of the hash.
func (h *Hash) UnmarshalJSON(b []byte) error {
	// *2 because there are 2 hex characters per byte.
	// +2 because the encoded JSON string has a `"` added at the beginning and end.
	if len(b) != HashSize*2+2 {
		return ErrHashWrongLen
	}

	var bstr string
	if err := json.Unmarshal(b, &bstr); err != nil {
		return err
	}
	return h.LoadString(bstr)
}

// Bytes convert Hash type to slice bytes
func (h Hash) Bytes() []byte {
	return h[:]
}

// ConvertBytesToHash return Hash type from slice byte
func ConvertBytesToHash(b []byte) Hash {
	var h Hash
	copy(h[:], b)
	return h
}

// ConvertBytesToHashes return []Hash type from slices of bytes.
func ConvertBytesToHashes(b [][]byte) []Hash {
	res := make([]Hash, len(b))
	for i, item := range b {
		var h Hash
		copy(h[:], item)
		res[i] = h
	}

	return res
}

// ConvertHashesToByteSlices takes hash slice and convert to slice bytes slices
func ConvertHashesToByteSlices(h []Hash) [][]byte {
	var res [][]byte
	for _, h := range h {
		res = append(res, h.Bytes())
	}
	return res
}
