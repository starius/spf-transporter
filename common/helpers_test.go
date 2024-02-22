package common

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"
)

func TestSolanaAddressEncoding(t *testing.T) {
	f := fuzz.New()
	for i := 0; i < 10; i++ {
		var addr SolanaAddress
		f.Fuzz(&addr)
		ad := PutSolanaAddress(addr)
		gotAddr, err := ExtractSolanaAddress(ad)
		require.NoError(t, err)
		require.Equal(t, addr, gotAddr)
		t.Logf("addr: %s; arbitrary data: %s", addr.String(), string(ad))
	}
}
