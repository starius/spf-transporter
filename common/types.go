package common

import (
	"errors"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

var ErrNotExists = errors.New("not exists")

type TransportType int

const (
	Airdrop TransportType = iota
	Premined
	Regular
)

type TransportStatus int

const (
	NotFound TransportStatus = iota
	Unconfirmed
	InTheQueue
	SolanaTxCreated
	Completed
)

type PublicKey string

type SupplyInfo struct {
	Airdrop  types.Currency `json:"airdrop_supply"`
	Premined types.Currency `json:"premined_supply"`
	Regular  types.Currency `json:"regular_supply"`
}

type SpfUtxo struct {
	Value      types.Currency   `json:"value"`
	UnlockHash types.UnlockHash `json:"unlockhash"`
}

type PreminedRecord struct {
	Limit       types.Currency `json:"limit"`
	Transported types.Currency `json:"transported"`
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

type TransportRequest struct {
	SpfxInvoice
	BurnID      types.TransactionID `json:"burn_id"`
	BurnTime    time.Time           `json:"burn_time"`
	QueueUpTime *time.Time          `json:"queue_up_time,omitempty"`
	Type        TransportType       `json:"type"`
}

type SolanaTxID string

type SolanaTxStatus struct {
	Confirmed      bool `json:"confirmed"`
	MintSuccessful bool `json:"mint_successful"`
}

type SolanaTxInfo struct {
	BroadcastTime time.Time  `json:"broadcast_time"`
	SolanaTx      SolanaTxID `json:"solana_tx_id"`
}

type TransportRecord struct {
	TransportRequest
	SolanaTxInfo
	ConfirmationTime time.Time `json:"confirmation_time"`
	Completed        bool      `json:"completed"`
}

type UnconfirmedTxInfo struct {
	BurnID       types.TransactionID `json:"burn_id"`
	Amount       types.Currency      `json:"amount"`
	SolanaAddr   SolanaAddress       `json:"solana_addr"`
	Time         time.Time           `json:"time"`
	PreminedAddr *types.UnlockHash   `json:"premined_addr"`
	Type         TransportType       `json:"type"`
}

type UtxoValidationResult struct {
	TransportAllowed bool            `json:"transport_allowed"`
	MaxAmount        *types.Currency `json:"max_amount,omitempty"`
	SupplyAfter      *types.Currency `json:"supply_after,omitempty"`
}

type ScpSubmitInfo struct {
	SupplyNeeded  types.Currency `json:"supply_needed"`
	CurrentSupply types.Currency `json:"current_supply"`
}
