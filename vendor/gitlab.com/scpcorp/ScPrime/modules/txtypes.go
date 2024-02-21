package modules

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"gitlab.com/scpcorp/ScPrime/crypto"
	"gitlab.com/scpcorp/ScPrime/types"
)

// TXType is the type of transaction determined by the contents
// has String() and JSON marshalling functions
type TXType int

// TXType constants for recognized transaction types
const (
	TXTypeSetup TXType = iota
	TXTypeMiner
	TXTypeSCPMove
	TXTypeSPFMove
	TXTypeFileContractNew
	TXTypeFileContractRenew
	TXTypeContractRevisionRegular
	TXTypeContractRevisionForRenew
	TXTypeStorageProof
	TXTypeHostAnnouncement
	TXTypeArbitraryData
	TXTypeMixed
	TXTypeSwapSPF
)

var (
	descriptionTXType = [...]string{
		"setup",
		"miner",
		"scp_transfer",
		"spf_transfer",
		"new_file_contract",
		"renew_file_contract",
		"contract_rev_gen",
		"contract_rev_renew",
		"storageproof",
		"hostannounce",
		"arbitrary_data",
		"composite",
		"swap_spf_scp",
	}
)

func (tt TXType) String() string {
	//Check if known
	i := int(tt)
	if i >= len(descriptionTXType) || i < 0 {
		return fmt.Sprintf("Nonexisting TXType specified by index %v.", i)
	}
	return descriptionTXType[tt]
}

// MarshalJSON puts the string representation of TXType instead of int value
func (tt TXType) MarshalJSON() ([]byte, error) {
	//Check if known
	i := int(tt)
	if i >= len(descriptionTXType) || i < 0 {
		return nil, fmt.Errorf("Cannot marshal unknown TXType (index %v)", i)
	}
	return json.Marshal(tt.String())
}

// UnmarshalJSON parses the input as string and converts it to the coded int constant
func (tt *TXType) UnmarshalJSON(b []byte) error {
	typeString := string(b)
	typeString, err := strconv.Unquote(typeString)
	if err != nil {
		return fmt.Errorf("Error %v processing TXType %v", err, typeString)
	}
	for i, existingtype := range descriptionTXType {
		if existingtype == typeString {
			*tt = TXType(i)
			return nil
		}
	}
	return fmt.Errorf("TXType %s is not recognized", typeString)
}

// TransactionType returns the transaction type determined by the transaction
// contents. Sanity or validity of transaction is not checked as that is done
// by transactionpool when transactions get submitted
func TransactionType(t *types.Transaction) TXType {
	if len(t.SiacoinInputs)+len(t.SiafundInputs)+
		len(t.FileContracts)+len(t.FileContractRevisions)+
		len(t.StorageProofs) == 0 {
		return TXTypeMiner
	}
	if len(t.FileContracts)+len(t.FileContractRevisions)+len(t.StorageProofs) > 1 {
		return TXTypeMixed
	}
	if len(t.FileContracts) > 0 {
		for _, contract := range t.FileContracts {
			if contract.FileMerkleRoot == (crypto.Hash{}) {
				return TXTypeFileContractNew
			}
		}
		return TXTypeFileContractRenew
	}
	if len(t.FileContractRevisions) > 0 {
		for _, revision := range t.FileContractRevisions {
			if revision.NewRevisionNumber == math.MaxUint64 {
				if len(revision.NewMissedProofOutputs) < 3 {
					return TXTypeContractRevisionForRenew
				}
			}
			return TXTypeContractRevisionRegular
		}
	}
	if len(t.StorageProofs) > 0 {
		return TXTypeStorageProof
	}
	if len(t.MinerFees) == 0 {
		return TXTypeSetup
	}
	if len(t.SiafundInputs) > 0 {
		//scheck for SCP to SPF Swap
		if IsSwapSPFtransaction(t) {
			return TXTypeSwapSPF
		}

		return TXTypeSPFMove
	}
	if len(t.SiacoinOutputs) == 0 {
		if len(t.ArbitraryData) > 0 {
			for _, arb := range t.ArbitraryData {
				_, _, err := DecodeAnnouncement(arb)
				if err == nil {
					return TXTypeHostAnnouncement
				}
			}
			return TXTypeArbitraryData
		}
	}
	return TXTypeSCPMove
}

// IsSwapSPFtransaction checks if it looks like SPF to SCP swap transaction
func IsSwapSPFtransaction(t *types.Transaction) bool {
	//check if spf outputs
	if len(t.SiafundOutputs) > 0 && len(t.SiacoinOutputs) > 0 {
		spfsenderaddress := t.SiacoinOutputs[0].UnlockHash
		scpsenderaddress := t.SiafundOutputs[0].UnlockHash
		for _, spfo := range t.SiafundOutputs {
			isSwap := spfsenderaddress == spfo.UnlockHash
			if isSwap {
				for _, scpo := range t.SiacoinOutputs {
					if scpsenderaddress == scpo.UnlockHash {
						return true
					}
				}
			}
		}
	}

	return false
}
