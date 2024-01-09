package transporter

import (
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

type PublicKey string

type SpfUtxo struct {
	Value      types.Currency   `json:"value"`
	UnlockHash types.UnlockHash `json:"unlockhash"`
}

type SolanaAddress string

type SpfxInvoice struct {
	Address     SolanaAddress  `json:"address"`
	Amount      types.Currency `json:"amount"`
	TotalSupply types.Currency `json:"total_supply"`
}

func (inv *SpfxInvoice) SupplyAfter() types.Currency {
	return inv.TotalSupply.Add(inv.Amount)
}

type SignedSpfxInvoice struct {
	SpfxInvoice
	Signature []byte `json:"signature"`
	Airdrop   bool   `json:"airdrop"`
}

type TransportRequest struct {
	SpfxInvoice
	BurnID      types.TransactionID `json:"burn_id"`
	BurnTime    time.Time           `json:"burn_time"`
	QueueUpTime time.Time           `json:"queue_up_time"`
}

type SolanaTxID string

type SolanaTransaction struct {
}

func (t *SolanaTransaction) ID() SolanaTxID {
	return SolanaTxID("")
}

type SolanaTxStatus struct {
	Confirmed      bool `json:"confirmed"`
	MintSuccessful bool `json:"mint_successful"`
}

type SolanaTxInfo struct {
	InvoiceTime  time.Time  `json:"invoice_time"`
	SolanaTxTime time.Time  `json:"solana_tx_time"`
	SolanaTx     SolanaTxID `json:"solana_tx_id"`
}

type TransportRecord struct {
	TransportRequest
	SolanaTxInfo
	Completed bool `json:"completed"`
}

type TransportStatus int

const (
	NotFound TransportStatus = iota
	InTheQueue
	SolanaTxCreated
	Completed
)
