package types

import (
	"encoding/hex"
	"fmt"

	"gitlab.com/scpcorp/ScPrime/crypto"
)

const (
	tokenNameSize = 16
)

// SectorWithToken is a combination of sector ID and a token.
type SectorWithToken struct {
	Token    TokenID
	SectorID crypto.Hash
}

// TokenID represent token type
type TokenID [tokenNameSize]byte

// String prints the id in hex.
func (t TokenID) String() string {
	return fmt.Sprintf("%x", t[:])
}

// ParseToken parse token from string
func ParseToken(t string) TokenID {
	tokenBytes, _ := hex.DecodeString(t)
	var token [16]byte
	copy(token[:], tokenBytes[:])
	return token
}

// Bytes convert TokenID to byte slice
func (t TokenID) Bytes() []byte {
	return t[:]
}
