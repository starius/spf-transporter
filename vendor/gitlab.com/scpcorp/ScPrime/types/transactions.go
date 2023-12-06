package types

// transaction.go defines the transaction type and all of the sub-fields of the
// transaction, as well as providing helper functions for working with
// transactions. The various IDs are designed such that, in a legal blockchain,
// it is cryptographically unlikely that any two objects would share an id.

import (
	"errors"
	"strings"

	"gitlab.com/NebulousLabs/encoding"
	"gitlab.com/scpcorp/ScPrime/crypto"
)

const (
	// UnlockHashChecksumSize is the size of the checksum used to verify
	// human-readable addresses. It is not a crypytographically secure
	// checksum, it's merely intended to prevent typos. 6 is chosen because it
	// brings the total size of the address to 38 bytes, leaving 2 bytes for
	// potential version additions in the future.
	UnlockHashChecksumSize = 6
)

// These Specifiers are used internally when calculating a type's ID. See
// Specifier for more details.
var (
	ErrTransactionIDWrongLen = errors.New("input has wrong length to be an encoded transaction id")

	SpecifierClaimOutput          = NewSpecifier("claim output")
	SpecifierFileContract         = NewSpecifier("file contract")
	SpecifierFileContractRevision = NewSpecifier("file contract re")
	SpecifierMinerFee             = NewSpecifier("miner fee")
	SpecifierMinerPayout          = NewSpecifier("miner payout")
	SpecifierSiacoinInput         = NewSpecifier("siacoin input")
	SpecifierSiacoinOutput        = NewSpecifier("siacoin output")
	SpecifierSiafundInput         = NewSpecifier("siafund input")
	SpecifierSiafundBInput        = NewSpecifier("siafundb input")
	SpecifierSiafundOutput        = NewSpecifier("siafund output")
	SpecifierSiafundBOutput       = NewSpecifier("siafundb output")
	SpecifierStorageProofOutput   = NewSpecifier("storage proof")
	SpecifierSiafundB             = NewSpecifier("siafund b")
)

type (
	// IDs are used to refer to a type without revealing its contents. They
	// are constructed by hashing specific fields of the type, along with a
	// Specifier. While all of these types are hashes, defining type aliases
	// gives us type safety and makes the code more readable.

	// TransactionID uniquely identifies a transaction
	TransactionID crypto.Hash
	// SiacoinOutputID uniquely identifies a siacoin output
	SiacoinOutputID crypto.Hash
	// SiafundOutputID uniquely identifies a siafund output
	SiafundOutputID crypto.Hash
	// FileContractID uniquely identifies a file contract
	FileContractID crypto.Hash
	// OutputID uniquely identifies an output
	OutputID crypto.Hash

	// A Transaction is an atomic component of a block. Transactions can contain
	// inputs and outputs, file contracts, storage proofs, and even arbitrary
	// data. They can also contain signatures to prove that a given party has
	// approved the transaction, or at least a particular subset of it.
	//
	// Transactions can depend on other previous transactions in the same block,
	// but transactions cannot spend outputs that they create or otherwise be
	// self-dependent.
	Transaction struct {
		SiacoinInputs         []SiacoinInput         `json:"siacoininputs"`
		SiacoinOutputs        []SiacoinOutput        `json:"siacoinoutputs"`
		FileContracts         []FileContract         `json:"filecontracts"`
		FileContractRevisions []FileContractRevision `json:"filecontractrevisions"`
		StorageProofs         []StorageProof         `json:"storageproofs"`
		SiafundInputs         []SiafundInput         `json:"siafundinputs"`
		SiafundOutputs        []SiafundOutput        `json:"siafundoutputs"`
		MinerFees             []Currency             `json:"minerfees"`
		ArbitraryData         [][]byte               `json:"arbitrarydata"`
		TransactionSignatures []TransactionSignature `json:"transactionsignatures"`
	}

	// A SiacoinInput consumes a SiacoinOutput and adds the siacoins to the set of
	// siacoins that can be spent in the transaction. The ParentID points to the
	// output that is getting consumed, and the UnlockConditions contain the rules
	// for spending the output. The UnlockConditions must match the UnlockHash of
	// the output.
	SiacoinInput struct {
		ParentID         SiacoinOutputID  `json:"parentid"`
		UnlockConditions UnlockConditions `json:"unlockconditions"`
	}

	// A SiacoinOutput holds a volume of siacoins. Outputs must be spent
	// atomically; that is, they must all be spent in the same transaction. The
	// UnlockHash is the hash of the UnlockConditions that must be fulfilled
	// in order to spend the output.
	SiacoinOutput struct {
		Value      Currency   `json:"value"`
		UnlockHash UnlockHash `json:"unlockhash"`
	}

	// A SiafundInput consumes a SiafundOutput and adds the siafunds to the set of
	// siafunds that can be spent in the transaction. The ParentID points to the
	// output that is getting consumed, and the UnlockConditions contain the rules
	// for spending the output. The UnlockConditions must match the UnlockHash of
	// the output.
	SiafundInput struct {
		ParentID         SiafundOutputID  `json:"parentid"`
		UnlockConditions UnlockConditions `json:"unlockconditions"`
		ClaimUnlockHash  UnlockHash       `json:"claimunlockhash"`
	}

	// A SiafundOutput holds a volume of siafunds. Outputs must be spent
	// atomically; that is, they must all be spent in the same transaction. The
	// UnlockHash is the hash of a set of UnlockConditions that must be fulfilled
	// in order to spend the output.
	//
	// When the SiafundOutput is spent, a SiacoinOutput is created, where:
	//
	//     SiacoinOutput.Value := (SiafundPool - ClaimStart) / 10,000 * Value
	//     SiacoinOutput.UnlockHash := SiafundInput.ClaimUnlockHash
	//
	// When a SiafundOutput is put into a transaction, the ClaimStart must always
	// equal zero. While the transaction is being processed, the ClaimStart is set
	// to the value of the SiafundPool.
	SiafundOutput struct {
		Value      Currency   `json:"value"`
		UnlockHash UnlockHash `json:"unlockhash"`
		ClaimStart Currency   `json:"claimstart"`
	}

	// SiafundClaim describes claim earned by SiafundOutput.
	// `Total` field is cumulative, `ByOwner` is how much goes to SiafundInput's ClaimUnlockHash.
	// For SPF-A, it's always the same thing, for SPF-B the difference goes to DevFund address.
	SiafundClaim struct {
		Total   Currency `json:"total"`
		ByOwner Currency `json:"byowner"`
	}

	// An UnlockHash is a specially constructed hash of the UnlockConditions type.
	// "Locked" values can be unlocked by providing the UnlockConditions that hash
	// to a given UnlockHash. See UnlockConditions.UnlockHash for details on how the
	// UnlockHash is constructed.
	UnlockHash crypto.Hash
)

// ID returns the id of a transaction, which is taken by marshalling all of the
// fields except for the signatures and taking the hash of the result.
func (t Transaction) ID() TransactionID {
	// Get the transaction id by hashing all data minus the signatures.
	var txid TransactionID
	h := crypto.NewHash()
	t.MarshalSiaNoSignatures(h)
	h.Sum(txid[:0])
	return txid
}

// RuneToString converts a rune type to a string.
func RuneToString(r rune) string {
	var sb strings.Builder
	sb.WriteRune(r)
	return sb.String()
}

// SiacoinOutputID returns the ID of a siacoin output at the given index,
// which is calculated by hashing the concatenation of the SiacoinOutput
// Specifier, all of the fields in the transaction (except the signatures),
// and output index.
func (t Transaction) SiacoinOutputID(i uint64) SiacoinOutputID {
	// Create the id.
	var id SiacoinOutputID
	h := crypto.NewHash()
	h.Write(SpecifierSiacoinOutput[:])
	t.MarshalSiaNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// FileContractID returns the ID of a file contract at the given index, which
// is calculated by hashing the concatenation of the FileContract Specifier,
// all of the fields in the transaction (except the signatures), and the
// contract index.
func (t Transaction) FileContractID(i uint64) FileContractID {
	var id FileContractID
	h := crypto.NewHash()
	h.Write(SpecifierFileContract[:])
	t.MarshalSiaNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// SiafundOutputID returns the ID of a SiafundOutput at the given index, which
// is calculated by hashing the concatenation of the SiafundOutput Specifier,
// all of the fields in the transaction (except the signatures), and output
// index.
func (t Transaction) SiafundOutputID(i uint64) SiafundOutputID {
	var id SiafundOutputID
	h := crypto.NewHash()
	h.Write(SpecifierSiafundOutput[:])
	t.MarshalSiaNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// SiacoinOutputSum returns the sum of all the siacoin outputs in the
// transaction, which must match the sum of all the siacoin inputs. Siacoin
// outputs created by storage proofs and siafund outputs are not considered, as
// they were considered when the contract responsible for funding them was
// created.
func (t Transaction) SiacoinOutputSum() (sum Currency) {
	// Add the siacoin outputs.
	for _, sco := range t.SiacoinOutputs {
		sum = sum.Add(sco.Value)
	}

	// Add the file contract payouts.
	for _, fc := range t.FileContracts {
		sum = sum.Add(fc.Payout)
	}

	// Add the miner fees.
	for _, fee := range t.MinerFees {
		sum = sum.Add(fee)
	}

	return
}

// HostSignature returns the host's transaction signature
func (t Transaction) HostSignature() TransactionSignature {
	return t.TransactionSignatures[1]
}

// RenterSignature returns the host's transaction signature
func (t Transaction) RenterSignature() TransactionSignature {
	return t.TransactionSignatures[0]
}

// SponsorAddresses returns a list of unique unlock hashes from transaction's inputs.
func (t Transaction) SponsorAddresses() []UnlockHash {
	inputUnlockHahes := make([]UnlockHash, 0, len(t.SiacoinInputs))
	uniqueUnlockHashes := make(map[UnlockHash]bool, len(t.SiacoinInputs))
	for _, in := range t.SiacoinInputs {
		uh := in.UnlockConditions.UnlockHash()
		if _, ok := uniqueUnlockHashes[uh]; ok {
			continue
		}
		uniqueUnlockHashes[uh] = true
		inputUnlockHahes = append(inputUnlockHahes, uh)
	}
	return inputUnlockHahes
}

// SiaClaimOutputID returns the ID of the SiacoinOutput that is created when
// the siafund output is spent. The ID is the hash the SiafundOutputID.
func (id SiafundOutputID) SiaClaimOutputID() SiacoinOutputID {
	return SiacoinOutputID(crypto.HashObject(id))
}

// SiaClaimSecondOutputID returns the ID of the second SiacoinOutput that is created
// for SPF-B when it is spent.
func (id SiafundOutputID) SiaClaimSecondOutputID() SiacoinOutputID {
	var soid SiacoinOutputID
	h := crypto.NewHash()
	h.Write(SpecifierSiafundB[:])
	h.Write(id[:])
	h.Sum(soid[:0])
	return soid
}

// Add adds sfc2 to sfc.
func (sfc *SiafundClaim) Add(sfc2 SiafundClaim) {
	sfc.Total = sfc.Total.Add(sfc2.Total)
	sfc.ByOwner = sfc.ByOwner.Add(sfc2.ByOwner)
}

// MulCurrency multiplies sfc by c.
func (sfc *SiafundClaim) MulCurrency(c Currency) {
	sfc.Total = sfc.Total.Mul(c)
	sfc.ByOwner = sfc.ByOwner.Mul(c)
}

// SiafundBLostClaim returns unclaimed amount for
// specific SPF-B claim lost due to lack of active contracts.
func SiafundBLostClaim(sfc SiafundClaim) Currency {
	// Workaround to handle negative currency due to bug in Fork2022,
	// fixed by SpfPoolHistoryHardfork.
	var lostClaim Currency
	if sfc.Total.Cmp(sfc.ByOwner) < 0 {
		lostClaim = ZeroCurrency
	} else {
		lostClaim = sfc.Total.Sub(sfc.ByOwner)
	}
	return lostClaim
}
