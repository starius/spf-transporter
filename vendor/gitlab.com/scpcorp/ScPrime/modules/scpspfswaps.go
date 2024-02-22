package modules

import (
	"gitlab.com/scpcorp/ScPrime/types"
)

// A SwapOffer is a transaction offer to swap SCP for SPF between
// two parties. When the offer is finalized the first output of both scpoutputs
// and spfoutputs is the receiver and second output is the change return if needed
// Transaction fee (miner fee) is paid by the party adding SCP to the transaction
// Offered and Accepted types can be "SCP","SPF-A","SPF-B"
type SwapOffer struct {
	OfferedFunds   string                       `json:"offered"`
	AcceptedFunds  string                       `json:"accepted"`
	SCPInputs      []types.SiacoinInput         `json:"scpinputs"`
	SPFInputs      []types.SiafundInput         `json:"spfinputs"`
	SCPOutputs     []types.SiacoinOutput        `json:"scpoutputs"`
	SPFOutputs     []types.SiafundOutput        `json:"spfoutputs"`
	TransactionFee types.Currency               `json:"transactionfee"`
	Signatures     []types.TransactionSignature `json:"signatures"`
}

// Transaction converts the swap transaction offer into a full transaction.
func (swap *SwapOffer) Transaction() types.Transaction {
	return types.Transaction{
		SiacoinInputs:         swap.SCPInputs,
		SiafundInputs:         swap.SPFInputs,
		SiacoinOutputs:        swap.SCPOutputs,
		SiafundOutputs:        swap.SPFOutputs,
		MinerFees:             []types.Currency{swap.TransactionFee},
		TransactionSignatures: swap.Signatures,
	}
}

// OfferedTypeSpecifier tells the type of funds offered
// Possible types are
// types.SpecifierSiafundOutput for SPF-A,
// types.SpecifierSiafundBOutput for SPF-B and
// types.SpecifierSiacoinOutput for SCP
func (swap *SwapOffer) OfferedTypeSpecifier() types.Specifier {
	switch swap.OfferedFunds {
	case "SPF-B":
		return types.SpecifierSiafundBOutput
	case "SPF-A":
		return types.SpecifierSiafundOutput
	}
	return types.SpecifierSiacoinOutput
}

// AcceptedTypeSpecifier tells the type of funds accepted
// Possible types are
// types.SpecifierSiafundOutput for SPF-A,
// types.SpecifierSiafundBOutput for SPF-B and
// types.SpecifierSiacoinOutput for SCP
func (swap *SwapOffer) AcceptedTypeSpecifier() types.Specifier {
	switch swap.AcceptedFunds {
	case "SPF-B":
		return types.SpecifierSiafundBOutput
	case "SPF-A":
		return types.SpecifierSiafundOutput
	}
	return types.SpecifierSiacoinOutput
}

// A SwapSummary shows the amount of SCP and SPF exchanged and the status of the
// offer
type SwapSummary struct {
	ReceiveSPF bool           `json:"receiveSPF"`
	ReceiveSCP bool           `json:"receiveSCP"`
	AmountSPF  types.Currency `json:"amountSPF"`
	AmountSCP  types.Currency `json:"amountSCP"`
	MinerFee   types.Currency `json:"minerFee"`
	Status     string         `json:"status"`
}
