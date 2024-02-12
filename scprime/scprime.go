package scprime

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"gitlab.com/scpcorp/ScPrime/modules"
	"gitlab.com/scpcorp/ScPrime/modules/consensus"
	"gitlab.com/scpcorp/ScPrime/modules/gateway"
	"gitlab.com/scpcorp/ScPrime/modules/transactionpool"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

type ScPrime struct {
	g  modules.Gateway
	cs modules.ConsensusSet
	tp modules.TransactionPool
}

func New(dir string) (*ScPrime, error) {
	g, err := gateway.New(":9381", true, filepath.Join(dir, "gateway"))
	if err != nil {
		return nil, err
	}
	var errChan <-chan error
	cs, errChan := consensus.New(g, true, filepath.Join(dir, "consensus"))
	if err != nil {
		return nil, err
	}
	log.Print("Syncing consensus, might take some time...")
	for !cs.Synced() {
		select {
		case err := <-errChan:
			return nil, fmt.Errorf("consensus initialization error: %w", err)
		case <-time.After(time.Second):
		}
	}
	log.Print("Done, consensus is synced.")
	tp, err := transactionpool.New(cs, g, filepath.Join(dir, "tpool"))
	if err != nil {
		return nil, err
	}
	return &ScPrime{g: g, cs: cs, tp: tp}, nil
}

func (s *ScPrime) IsInTransactionPool(id types.TransactionID) bool {
	_, _, exists := s.tp.Transaction(id)
	return exists
}

func (s *ScPrime) IsTxConfirmed(id types.TransactionID) (bool, error) {
	return s.tp.TransactionConfirmed(id)
}

func (s *ScPrime) ValidTransaction(tx *types.Transaction) bool {
	for _, sig := range tx.TransactionSignatures {
		if !sig.CoveredFields.WholeTransaction {
			return false
		}
	}
	currnetHeight := s.cs.Height()
	if err := tx.StandaloneValid(currnetHeight); err != nil {
		return false
	}
	return true
}

func (s *ScPrime) ExtractSolanaAddress(tx *types.Transaction) (common.SolanaAddress, error) {
	if len(tx.ArbitraryData) != 1 {
		return nil, fmt.Errorf("length of ArbitraryData must be 1, got %d", len(tx.ArbitraryData))
	}
	if len(tx.ArbitraryData[0]) != common.SolanaAddressLen {
		return nil, fmt.Errorf("incorrect solana address len %d, must be 32", len(tx.ArbitraryData[0]))
	}
	return common.SolanaAddress(tx.ArbitraryData[0]), nil
}

func (s *ScPrime) Close() error {
	if err := s.tp.Close(); err != nil {
		return fmt.Errorf("failed to close transaction pool: %w", err)
	}
	if err := s.cs.Close(); err != nil {
		return fmt.Errorf("failed to close consensus: %w", err)
	}
	if err := s.g.Close(); err != nil {
		return fmt.Errorf("failed to close gateway: %w", err)
	}
	return nil
}
