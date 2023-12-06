package api

import (
	"context"
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

type EmissionRules interface {
	Time(totalSuply types.Currency) time.Time
}

type ScpBlockchain interface {
	IsTxConfirmed(id types.TransactionID) bool
}

type Server struct {
	emission      EmissionRules
	scpBlockchain ScpBlockchain
}

func New(emission EmissionRules, scpBlockchain ScpBlockchain) *Server {
	return &Server{
		emission:      emission,
		scpBlockchain: scpBlockchain,
	}
}

func (s *Server) CheckUtxoApproval(ctx context.Context, req *CheckUtxoApprovalRequest) (*CheckUtxoApprovalResponse, error) {
	return &CheckUtxoApprovalResponse{}, nil
}

func (s *Server) SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error) {
	return &SubmitScpTxResponse{}, nil
}

func (s *Server) SubmitSolanaTx(ctx context.Context, req *SubmitSolanaTxRequest) (*SubmitSolanaTxResponse, error) {
	return &SubmitSolanaTxResponse{}, nil
}

func (s *Server) History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error) {
	return &HistoryResponse{}, nil
}
