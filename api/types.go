package api

//go:generate go run ./gen/...

import (
	"context"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

type PublicKey string

type Service interface {
	CheckUtxoApproval(ctx context.Context, req *CheckUtxoApprovalRequest) (*CheckUtxoApprovalResponse, error)
	SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error)
	SubmitSolanaTx(ctx context.Context, req *SubmitSolanaTxRequest) (*SubmitSolanaTxResponse, error)
	History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error)
}

type SpfUtxo struct {
	Value      types.Currency   `json:"value"`
	UnlockHash types.UnlockHash `json:"unlockhash"`
}

type CheckUtxoApprovalRequest struct {
	Utxos []SpfUtxo `json:"utxos"`
}

type CheckUtxoApprovalResponse struct {
	TransportAllowed bool           `json:"transport_allowed"`
	WaitTime         *time.Duration `json:"wait_time"`
}

type SubmitScpTxRequest struct {
	BurnID          *types.TransactionID `json:"burn_id"`
	SpfxTotalSupply *types.Currency      `json:"spfx_total_supply"`
}

type SolanaAddress string

type SpfxInvoice struct {
	Address     SolanaAddress  `json:"address"`
	Amount      types.Currency `json:"amount"`
	TotalSupply types.Currency `json:"total_supply"`
}

type SubmitScpTxResponse struct {
	WaitTime   *time.Duration `json:"wait_time"`
	Invoice    *SpfxInvoice   `json:"spfx_invoice"`
	InvoiceSig []byte         `json:"invoice_sig"`
}

type SolanaTxID string

type SubmitSolanaTxRequest struct {
	ID SolanaTxID `json:"id"`
}

type SubmitSolanaTxResponse struct {
}

type HistoryRequest struct {
	PageID string `json:"page_id"`
}

type TransportRecord struct {
	BurnTime     time.Time  `json:"burn_time"`
	QueueUpTime  time.Time  `json:"queue_up_time"`
	QueueEndTime time.Time  `json:"queue_end_time"`
	InvoiceTime  *time.Time `json:"invoice_time"`
	SolanaTxTime *time.Time `json:"solana_tx_time"`
}

type HistoryResponse struct {
	Records    []TransportRecord `json:"records"`
	NextPageID string            `json:"next_page_id"`
	More       bool              `json:"more"`
}

type Error struct {
	Msg string
}

func (err Error) Error() string {
	return err.Msg
}
