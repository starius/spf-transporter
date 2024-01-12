package transporter

import (
	"context"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

type Storage interface {
	CreateRecord(ctx context.Context, record *TransportRequest) error
	AddSolanaTransaction(ctx context.Context, info map[types.TransactionID]SolanaTxInfo) error
	Commit(ctx context.Context, ids []types.TransactionID) error
	Record(ctx context.Context, burnID types.TransactionID) (*TransportRecord, error)
	NextInQueue(ctx context.Context, currentSupply, allowedSupply types.Currency) ([]TransportRequest, error)
	PreminedWhitelist(ctx context.Context) ([]SpfUtxo, error)
}

type Solana interface {
	BuildTransaction(ctx context.Context, invoices []SignedSpfxInvoice) (SolanaTransaction, error)
	Broadcast(ctx context.Context, transactions []SolanaTransaction) error
	TxStatus(ctx context.Context, ids []SolanaTxID) []SolanaTxStatus
	TotalSupply(ctx context.Context) (types.Currency, error)
	EmittedSupply(ctx context.Context) (types.Currency, error) // Without airdrop funds.
}

type EmissionRules interface {
	AllowedSupply() types.Currency
	TimeTillSupply(supply types.Currency) (time.Duration, bool)
}

type ScpBlockchain interface {
	IsTxConfirmed(id types.TransactionID) bool
	ExtractSolanaAddress(id types.TransactionID) (SolanaAddress, error)
}

type Server struct {
	emission      EmissionRules
	scpBlockchain ScpBlockchain
	solana        Solana
	storage       Storage
}

func New(emission EmissionRules, scpBlockchain ScpBlockchain, solana Solana, storage Storage) *Server {
	return &Server{
		emission:      emission,
		scpBlockchain: scpBlockchain,
		solana:        solana,
		storage:       storage,
	}
}

func (s *Server) CheckUtxoApproval(ctx context.Context, req *CheckUtxoApprovalRequest) (*CheckUtxoApprovalResponse, error) {
	return &CheckUtxoApprovalResponse{}, nil
}

func (s *Server) SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error) {
	return &SubmitScpTxResponse{}, nil
}

func (s *Server) TransportStatus(ctx context.Context, req *TransportStatusRequest) (*TransportStatusResponse, error) {
	return &TransportStatusResponse{}, nil
}

func (s *Server) History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error) {
	return &HistoryResponse{}, nil
}
