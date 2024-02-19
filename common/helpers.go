package common

import (
	"time"

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
