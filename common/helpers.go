package common

import (
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

func HasAirdrop(unlockHashes []types.UnlockHash) bool {
	for _, uh := range unlockHashes {
		if uh == AirdropUnlockHash {
			return true
		}
	}
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

func ExtractUnlockHashes(utxos []SpfUtxo) (res []types.UnlockHash) {
	uniqueUn := make(map[types.UnlockHash]bool)
	for _, utxo := range utxos {
		if _, ok := uniqueUn[utxo.UnlockHash]; !ok {
			uniqueUn[utxo.UnlockHash] = true
			res = append(res, utxo.UnlockHash)
		}
	}
	return
}

func SumUtxos(utxos []SpfUtxo) (sum types.Currency) {
	for _, utxo := range utxos {
		sum = sum.Add(utxo.Value)
	}
	return
}

func IsPreminedPeriod(now time.Time) bool {
	return now.Before(EmissionStart)
}

func DivCurrencyRoundUp(c, divBy types.Currency) types.Currency {
	return c.Add(divBy.Sub64(1)).Div(divBy)
}
