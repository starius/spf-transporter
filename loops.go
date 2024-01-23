package transporter

import (
	"context"
	"fmt"
	"log"
	"time"

	"gitlab.com/scpcorp/spf-transporter/common"
)

func (s *Server) runInALoop(ctx context.Context, name string, interval time.Duration, callback func(ctx context.Context) error) {
	s.stopWg.Add(1)
	ticker := time.NewTicker(interval)

	go func() {
		defer func() {
			ticker.Stop()
			s.stopWg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				log.Printf("%s loop done by context", name)
				return
			case <-ticker.C:
				if err := callback(ctx); err != nil {
					log.Printf("%s callback failed: %v", name, err)
				}
			}
		}
	}()
}

func (s *Server) processUnconfirmed(ctx context.Context) error {
	unconfirmedTxs, err := s.storage.UnconfirmedBefore(ctx, s.now().Add(-s.settings.ScpTxConfirmationTime))
	if err != nil {
		return fmt.Errorf("failed to fetch unconfirmed: %w", err)
	}
	var toRemove, toConfirm []common.UnconfirmedTxInfo
	for _, utx := range unconfirmedTxs {
		confirmed, err := s.scpBlockchain.IsTxConfirmed(utx.BurnID)
		if err != nil {
			return fmt.Errorf("failed to check if tx is confirmed: %w", err)
		}
		if !confirmed {
			log.Printf("%s burn tx is still not confirmed after %s, removing", utx.BurnID.String(), s.settings.ScpTxConfirmationTime.String())
			toRemove = append(toRemove, utx)
		} else {
			toConfirm = append(toConfirm, utx)
		}
	}
	if _, err := s.storage.ConfirmUnconfirmed(ctx, toConfirm, s.now()); err != nil {
		return fmt.Errorf("failed to confirm unconfirmed: %w", err)
	}
	if err := s.storage.RemoveUnconfirmed(ctx, toRemove); err != nil {
		return fmt.Errorf("failed to remove unconfirmed: %w", err)
	}
	return nil
}

// mint never returns an error as its failure would lead to inconsistent database state
// and further smart contract calls would fail anyway due to incorrect supply
// amounts. So if something goes wrong (which is only the case when database or
// solana contract are broken/unreachable, e.g. no network connection), this function
// blocks further processing of transport requests as it is impossible in such case anyway.
func (s *Server) mint(req common.TransportRequest) {
	ctx := context.Background()
	for {
		solanaTxID, solanaTx, err := s.solana.BuildTransaction(ctx, req.Type, []common.SpfxInvoice{req.SpfxInvoice})
		if err != nil {
			log.Printf("failed to build solana transaction: %v", err)
			continue
		}
		info := common.SolanaTxInfo{
			BroadcastTime: s.now(),
			SolanaTx:      solanaTxID,
		}
		if err := s.storage.AddSolanaTransaction(ctx, req.BurnID, req.Type, info); err != nil {
			log.Printf("failed to save solana transaction info: %v", err)
			continue
		}
		if err := s.solana.SendAndConfirm(ctx, solanaTx); err != nil {
			log.Printf("solana.SendAndConfirm failed (id %s), waiting %s (SolanaTxDecayTime) to ensure transaction is really gone...", string(solanaTxID), s.settings.SolanaTxDecayTime.String())
			time.Sleep(s.settings.SolanaTxDecayTime)
			status, err := s.solana.TxStatus(ctx, []common.SolanaTxID{solanaTxID})
			if err != nil {
				log.Printf("TxStatus returned an error: %v", err)
				continue
			}
			if !(status[0].Confirmed && status[0].MintSuccessful) {
				log.Printf("solana transaction %s was not found, build a new one and retry", string(solanaTxID))
				continue
			}
		}
		confirmTime := s.now()
		// After we know solana transaction is confirmed, we must ensure it is marked confirmed in the database.
		confirmErr := s.storage.ConfirmSolana(ctx, solanaTxID, confirmTime)
		for ; confirmErr != nil; confirmErr = s.storage.ConfirmSolana(ctx, solanaTxID, confirmTime) {
			log.Printf("failed to mark solana tx (%s) confirmed in the database: %v. Retying...", string(solanaTxID), confirmErr)
		}
		// Success.
		break
	}
}

func (s *Server) processMints(ctx context.Context) error {
	var totalReqs []common.TransportRequest
	preminedReqs, err := s.storage.UncompletedPremined(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch premined requests: %w", err)
	}
	totalReqs = append(totalReqs, preminedReqs...)
	airdropReqs, err := s.storage.UncompletedAirdrop(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch airdrop requests: %w", err)
	}
	totalReqs = append(totalReqs, airdropReqs...)
	allowedSupply := s.emission.AllowedSupply()
	queueReqs, err := s.storage.NextInQueue(ctx, allowedSupply)
	if err != nil {
		return fmt.Errorf("failed to fetch next queue records: %w", err)
	}
	totalReqs = append(totalReqs, queueReqs...)
	log.Printf("[processMints]: got %d requests to process (%d airdrop; %d premined; %d queue)...\n", len(totalReqs), len(airdropReqs), len(preminedReqs), len(queueReqs))
	for i, req := range totalReqs {
		s.mint(req)
		log.Printf("[processMints]: Processed %d/%d requests\n", i+1, len(totalReqs))
	}
	return nil
}

const queueLockedFlag = "queue_locked"

// checkQueue handles `ReleaseAllAtOnce` queue mode.
func (s *Server) checkQueue(ctx context.Context) error {
	queueSize, err := s.storage.QueueSize(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch queue size: %w", err)
	}
	locked, err := s.isQueueLocked(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to check queue lock: %w", err)
	}
	if locked && queueSize.IsZero() {
		// Need to release the queue lock.
		if err := s.setQueueLocked(ctx, false); err != nil {
			return fmt.Errorf("failed to set queue locked flag to false: %w", err)
		}
	} else if !locked && queueSize.Cmp(s.settings.QueueLockLimit) > 0 {
		if err := s.setQueueLocked(ctx, true); err != nil {
			return fmt.Errorf("failed to set queue locked flag to true: %w", err)
		}
	}
	return nil
}
