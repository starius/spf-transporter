package common

import (
	"fmt"
	"time"

	"gitlab.com/scpcorp/ScPrime/modules"
	"gitlab.com/scpcorp/ScPrime/types"
)

// Airdrop is not implemented yet.
func HasAirdrop(unlockHashes []types.UnlockHash) bool {
	/*for _, uh := range unlockHashes {
		if uh == AirdropUnlockHash {
			return true
		}
	}*/
	return false
}

func BurntAmount(tx *types.Transaction) types.Currency {
	var burntAmount types.Currency
	for _, sfo := range tx.SiafundOutputs {
		if sfo.UnlockHash == types.BurnAddressUnlockHash {
			burntAmount = burntAmount.Add(sfo.Value)
		}
	}
	return burntAmount
}

func ExtractSiafundUnlockHashes(tx *types.Transaction) (res []types.UnlockHash) {
	uniqueUn := make(map[types.UnlockHash]bool)
	for _, sfi := range tx.SiafundInputs {
		uh := sfi.UnlockConditions.UnlockHash()
		if _, ok := uniqueUn[uh]; !ok {
			uniqueUn[uh] = true
			res = append(res, uh)
		}
	}
	return
}

func IsPreminedPeriod(now time.Time) bool {
	return now.Before(EmissionStart)
}

func DivCurrencyRoundUp(c, divBy types.Currency) types.Currency {
	return c.Add(divBy.Sub64(1)).Div(divBy)
}

const SolanaAddressArbitraryDataLen = types.SpecifierLen*2 + SolanaAddrLen

var PrefixSolanaSpfAddr = types.NewSpecifier("SolanaSpfAddr")

// PutSolanaAddress creates ArbitraryData from Solana address.
func PutSolanaAddress(addr SolanaAddress) []byte {
	res := make([]byte, SolanaAddressArbitraryDataLen)
	copy(res[:], modules.PrefixNonSia[:])
	copy(res[types.SpecifierLen:], PrefixSolanaSpfAddr[:])
	copy(res[types.SpecifierLen*2:], addr[:])
	return res
}

// ExtractSolanaAddress extracts Solana address from ArbitraryData.
func ExtractSolanaAddress(arbitraryData []byte) (addr SolanaAddress, err error) {
	if len(arbitraryData) != SolanaAddressArbitraryDataLen {
		err = fmt.Errorf("length must be %d; got %d", SolanaAddressArbitraryDataLen, len(arbitraryData))
		return
	}
	var prefix0, prefix1 types.Specifier
	copy(prefix0[:], arbitraryData)
	if prefix0 != modules.PrefixNonSia {
		err = fmt.Errorf("invalid first prefix %s; need %s", prefix0.String(), modules.PrefixNonSia.String())
		return
	}
	copy(prefix1[:], arbitraryData[types.SpecifierLen:])
	if prefix1 != PrefixSolanaSpfAddr {
		err = fmt.Errorf("invalid second prefix %s; need %s", prefix1.String(), PrefixSolanaSpfAddr.String())
		return
	}
	copy(addr[:], arbitraryData[types.SpecifierLen*2:])
	return
}
