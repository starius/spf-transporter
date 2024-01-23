package transporter

//go:generate go run ./gen/...

import (
	"context"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

type Service interface {
	PreminedList(ctx context.Context, req *PreminedListRequest) (*PreminedListResponse, error)
	CheckAllowance(ctx context.Context, req *CheckAllowanceRequest) (*CheckAllowanceResponse, error)
	SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error)
	TransportStatus(ctx context.Context, req *TransportStatusRequest) (*TransportStatusResponse, error)
	History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error)
}

type PreminedListRequest struct {
}

type PreminedListResponse struct {
	Premined map[types.UnlockHash]common.PreminedRecord `json:"premined"`
}

type CheckAllowanceRequest struct {
	PreminedUnlockHashes *[]types.UnlockHash `json:"premined_unlock_hashes,omitempty"`
}

type AmountWithTimeEstimate struct {
	Amount       types.Currency `json:"amount"`
	WaitEstimate time.Duration  `json:"wait_estimate"`
}

type CheckAllowanceResponse struct {
	Airdrop  AmountWithTimeEstimate                      `json:"airdrop"`
	Premined map[types.UnlockHash]AmountWithTimeEstimate `json:"premined"`
	Regular  AmountWithTimeEstimate                      `json:"regular"`
}

type SubmitScpTxRequest struct {
	Transaction types.Transaction `json:"transaction"`
}

type SubmitScpTxResponse struct {
	WaitTimeEstimate time.Duration   `json:"wait_time_estimate"`
	SpfAmountAhead   *types.Currency `json:"spf_amount_ahead"`
}

type TransportStatusRequest struct {
	BurnID types.TransactionID `json:"burn_id"`
}

type TransportStatusResponse struct {
	Status common.TransportStatus `json:"status"`
}

type HistoryRequest struct {
	PageID string `json:"page_id"`
}

type HistoryResponse struct {
	Records    []common.TransportRecord `json:"records"`
	NextPageID string                   `json:"next_page_id"`
	More       bool                     `json:"more"`
}

type Error struct {
	Msg string
}

func (err Error) Error() string {
	return err.Msg
}
