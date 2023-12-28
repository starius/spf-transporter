package transporter

//go:generate go run ./gen/...

import (
	"context"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

type Service interface {
	CheckUtxoApproval(ctx context.Context, req *CheckUtxoApprovalRequest) (*CheckUtxoApprovalResponse, error)
	SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error)
	TransportStatus(ctx context.Context, req *TransportStatusRequest) (*TransportStatusResponse, error)
	History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error)
}

type CheckUtxoApprovalRequest struct {
	Utxos []SpfUtxo `json:"utxos"`
}

type CheckUtxoApprovalResponse struct {
	TransportAllowed bool            `json:"transport_allowed"`
	MaxAmount        *types.Currency `json:"max_amount"`
	WaitTimeEstimate *time.Duration  `json:"wait_time_estimate"`
}

type SubmitScpTxRequest struct {
	BurnID types.TransactionID `json:"burn_id"`
}

type SubmitScpTxResponse struct {
	WaitTimeEstimate *time.Duration `json:"wait_time_estimate"`
	QueuePosition    int            `json:"queue_position"`
	SpfAmountAhead   types.Currency `json:"spf_amount_ahead"`
}

type TransportStatusRequest struct {
	BurnID types.TransactionID `json:"burn_id"`
}

type TransportStatusResponse struct {
	Status TransportStatus `json:"status"`
}

type HistoryRequest struct {
	PageID string `json:"page_id"`
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
