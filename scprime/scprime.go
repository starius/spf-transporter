package scprime

import (
	"errors"
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
			if err != nil {
				return nil, fmt.Errorf("consensus initialization error: %w", err)
			}
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

func (s *ScPrime) CurrentHeight() (types.BlockHeight, error) {
	if !s.cs.Synced() {
		return types.BlockHeight(0), errors.New("consensus is not synced")
	}
	return s.cs.Height(), nil
}

func (s *ScPrime) ValidTransaction(tx *types.Transaction) error {
	for _, sig := range tx.TransactionSignatures {
		if !sig.CoveredFields.WholeTransaction {
			return errors.New("CoveredFields must have WholeTransaction flag set")
		}
	}
	currentHeight := s.cs.Height()
	if err := tx.StandaloneValid(currentHeight); err != nil {
		return err
	}
	return nil
}

func (s *ScPrime) ExtractSolanaAddress(tx *types.Transaction) (common.SolanaAddress, error) {
	for _, ad := range tx.ArbitraryData {
		addr, err := common.ExtractSolanaAddress(ad)
		if err != nil {
			continue
		}
		return addr, nil
	}
	return common.SolanaAddress{}, errors.New("no arbitrary data matching SolanaAddress was found")
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
