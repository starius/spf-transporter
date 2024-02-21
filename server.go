package transporter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

type Storage interface {
	QueueSize(ctx context.Context) (types.Currency, error)
	ConfirmedSupply(ctx context.Context) (common.SupplyInfo, error)
	PreminedLimits(ctx context.Context) (map[types.UnlockHash]common.PreminedRecord, error)
	FindPremined(ctx context.Context, unlockHashes []types.UnlockHash) (map[types.UnlockHash]common.PreminedRecord, error)
	CheckAllowance(ctx context.Context, preminedAddrs []types.UnlockHash) (*common.Allowance, error)
	AddUnconfirmedScpTx(ctx context.Context, info *common.UnconfirmedTxInfo) (*common.QueueAllowance, error)
	UnconfirmedBefore(ctx context.Context, before time.Time) ([]common.UnconfirmedTxInfo, error)
	SetConfirmationHeight(ctx context.Context, ids []types.TransactionID, height types.BlockHeight) error
	ConfirmUnconfirmed(ctx context.Context, txs []common.UnconfirmedTxInfo, curTime time.Time) ([]common.TransportRequest, error)
	RemoveUnconfirmed(ctx context.Context, txs []common.UnconfirmedTxInfo) error
	AddSolanaTransaction(ctx context.Context, burnID types.TransactionID, t common.TransportType, info common.SolanaTxInfo) error
	RemoveSolanaTransaction(ctx context.Context, burnID types.TransactionID, t common.TransportType) error
	ConfirmSolana(ctx context.Context, solanaID common.SolanaTxID, confirmTime time.Time) error
	UnconfirmedInfo(ctx context.Context, burnID types.TransactionID) (*common.UnconfirmedTxInfo, error)
	TransportRecord(ctx context.Context, burnID types.TransactionID) (*common.TransportRecord, error)
	NextInQueue(ctx context.Context, allowedSupply types.Currency) ([]common.TransportRequest, error)
	UncompletedPremined(ctx context.Context) ([]common.TransportRequest, error)
	UncompletedAirdrop(ctx context.Context) ([]common.TransportRequest, error)
	RecordsWithUnconfirmedSolana(ctx context.Context) ([]common.TransportRecord, error)
	SetFlag(ctx context.Context, name string, value bool) error
	GetFlag(ctx context.Context, name string) (bool, error)
	Close()
}

type Solana interface {
	CheckAddress(ctx context.Context, addr common.SolanaAddress, amount types.Currency, skipWalletAccountCheck bool) error
	BuildTransaction(ctx context.Context, t common.TransportType, invoices []common.SpfxInvoice) (common.SolanaTxID, *solana.Transaction, error)
	SendAndConfirm(ctx context.Context, transaction *solana.Transaction) error
	TxStatus(ctx context.Context, ids []common.SolanaTxID) ([]common.SolanaTxStatus, error)
	SupplyInfo(ctx context.Context) (common.SupplyInfo, error)
}

type EmissionRules interface {
	AllowedSupply() types.Currency
	EmissionTime(supplyChange types.Currency) time.Duration
}

type ScpBlockchain interface {
	IsInTransactionPool(id types.TransactionID) bool
	IsTxConfirmed(id types.TransactionID) (bool, error)
	ExtractSolanaAddress(tx *types.Transaction) (common.SolanaAddress, error)
	ValidTransaction(tx *types.Transaction) error
	CurrentHeight() (types.BlockHeight, error)
	Close() error
}

type Settings struct {
	SolanaTxDecayTime        time.Duration
	QueueCheckInterval       time.Duration
	UnconfirmedCheckInterval time.Duration
	ScpTxConfirmationTime    time.Duration
	ScpTxConfirmations       int

	TransportMin   types.Currency
	TransportMax   types.Currency
	QueueSizeLimit types.Currency
	QueueLockLimit types.Currency
	QueueLockGap   time.Duration
	QueueMode      common.QueueHandlingMode
}

type Server struct {
	emission      EmissionRules
	scpBlockchain ScpBlockchain
	solana        Solana
	storage       Storage

	settings *Settings
	now      func() time.Time

	queueLockedMu sync.Mutex
	queueLocked   *bool
	lockTime      time.Time

	cancel context.CancelFunc
	stopWg sync.WaitGroup // Close waits for this WaitGroup.
}

func New(settings *Settings, emission EmissionRules, scpBlockchain ScpBlockchain, solana Solana, storage Storage) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		settings:      settings,
		emission:      emission,
		scpBlockchain: scpBlockchain,
		solana:        solana,
		storage:       storage,
		now:           func() time.Time { return time.Now().UTC() },
		cancel:        cancel,
	}
	// Find solana transactions which were not marked confirmed.
	// Check each of them and either mark them confirmed or remove.
	if err := s.handleUnconfirmedSolana(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to handle records with unconfirmed solana txs: %w", err)
	}
	// Sanity supply check.
	if err := s.compareSupplyToSolana(context.Background()); err != nil {
		return nil, fmt.Errorf("supply mismatch, unable to launch server: %w", err)
	}
	// Start background loops.
	s.runInALoop(ctx, "process unconfirmed", settings.UnconfirmedCheckInterval, s.processUnconfirmed)
	s.runInALoop(ctx, "process mints", settings.QueueCheckInterval, s.processMints)
	if settings.QueueMode == common.ReleaseAllAtOnce {
		s.runInALoop(ctx, "process queue", settings.QueueCheckInterval, s.checkQueue)
	}
	return s, nil
}

func (s *Server) PreminedList(ctx context.Context, req *PreminedListRequest) (*PreminedListResponse, error) {
	list, err := s.storage.PreminedLimits(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch premined limits: %w", err)
	}
	respMap := make(map[string]common.PreminedRecord, len(list))
	for uh, r := range list {
		respMap[uh.String()] = r
	}
	return &PreminedListResponse{Premined: respMap}, nil
}

func (s *Server) setQueueLocked(ctx context.Context, val bool) error {
	s.queueLockedMu.Lock()
	defer s.queueLockedMu.Unlock()
	if err := s.storage.SetFlag(ctx, queueLockedFlag, val); err != nil {
		return fmt.Errorf("failed to set flag %s: %w", queueLockedFlag, err)
	}
	valCp := val
	s.queueLocked = &valCp
	if val {
		s.lockTime = s.now()
	}
	return nil
}

func (s *Server) getQueueLocked(ctx context.Context) (bool, time.Time, error) {
	s.queueLockedMu.Lock()
	defer s.queueLockedMu.Unlock()
	if s.queueLocked != nil {
		val := *s.queueLocked
		return val, s.lockTime, nil
	}
	locked, err := s.storage.GetFlag(ctx, queueLockedFlag)
	if err != nil {
		return false, s.lockTime, fmt.Errorf("failed to load flag %s: %w", queueLockedFlag, err)
	}
	s.queueLocked = &locked
	return locked, s.lockTime, nil
}

func (s *Server) isQueueLocked(ctx context.Context, hardCheck bool) (bool, error) {
	if s.settings.QueueMode == common.OneByOne {
		// Queue is never locked in one-by-one mode.
		// QueueSizeLimit it still checked later in both modes.
		return false, nil
	}
	locked, lockTime, err := s.getQueueLocked(ctx)
	if err != nil {
		return false, err
	}
	if lockTime.IsZero() || hardCheck {
		return locked, nil
	}
	if locked && (s.now().Sub(lockTime) < s.settings.QueueLockGap) {
		// Allow up to `QueueLockGap` time between CheckAllowance and SubmitScpTx methods.
		locked = false
	}
	return locked, nil
}

func (s *Server) compareSupplyToSolana(ctx context.Context) error {
	solanaSupply, err := s.solana.SupplyInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch solana supply information: %w", err)
	}
	transporterSupply, err := s.storage.ConfirmedSupply(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch supply information from database: %w", err)
	}
	if !solanaSupply.Equals(transporterSupply) {
		return fmt.Errorf("solana supply (%+v) not equals to transporter (%+v)", solanaSupply, transporterSupply)
	}
	return nil
}

func (s *Server) handleUnconfirmedSolana(ctx context.Context) error {
	unconfirmed, err := s.storage.RecordsWithUnconfirmedSolana(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch records with unconfirmed solana: %w", err)
	}
	if len(unconfirmed) == 0 {
		return nil
	}
	log.Printf("Found %d unconfirmed solana transactions, checking...", len(unconfirmed))
	// Wait enough time to ensure transactions are either totally gone or confirmed.
	time.Sleep(s.settings.SolanaTxDecayTime)
	solanaIDs := make([]common.SolanaTxID, 0, len(unconfirmed))
	for _, r := range unconfirmed {
		solanaIDs = append(solanaIDs, r.SolanaTx)
	}
	solanaStatus, err := s.solana.TxStatus(ctx, solanaIDs)
	if err != nil {
		return fmt.Errorf("solana tx status: %w", err)
	}
	for i, status := range solanaStatus {
		record := unconfirmed[i]
		if status.Confirmed && status.MintSuccessful {
			// Transaction is fine, mark confirmed in the database.
			if err := s.storage.ConfirmSolana(ctx, record.SolanaTx, status.ConfirmationTime); err != nil {
				return fmt.Errorf("failed to mark confirmed: %w", err)
			}
			log.Printf("Solana transaction %s is confirmed, successfully recovered", string(record.SolanaTx))
			continue
		}
		// Transaction is definitely stale, remove it and forget.
		log.Printf("Removing stale solana transaction %s (burnID %s)", record.SolanaTx, record.BurnID.String())
		if err := s.storage.RemoveSolanaTransaction(ctx, record.BurnID, record.Type); err != nil {
			return fmt.Errorf("failed to remove solana tx: %w", err)
		}
	}
	return nil
}

func (s *Server) validateAmount(burntAmount types.Currency, checkMax bool) error {
	if burntAmount.Cmp(s.settings.TransportMin) < 0 {
		return fmt.Errorf("amount %s is too small to be accepted, min is %s", burntAmount.String(), s.settings.TransportMin.String())
	}
	if checkMax && burntAmount.Cmp(s.settings.TransportMax) > 0 {
		return fmt.Errorf("amount %s is greater than the allowed max %s", burntAmount.String(), s.settings.TransportMax.String())
	}
	return nil
}

func (s *Server) checkUnlockHashes(ctx context.Context, unlockHashes []types.UnlockHash) (*common.TransportType, error) {
	airdrop := common.HasAirdrop(unlockHashes)
	if airdrop && len(unlockHashes) != 1 {
		return nil, fmt.Errorf("provided set includes airdrop unlock hash but has more than 1 unique UnlockHashes (%d)", len(unlockHashes))
	}
	preminedRecords, err := s.storage.FindPremined(ctx, unlockHashes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract premined: %w", err)
	}
	premined := (len(preminedRecords) != 0)
	if premined && len(unlockHashes) != 1 {
		return nil, fmt.Errorf("provided set includes premined unlock hashes but has more than 1 unique UnlockHashes (%d)", len(unlockHashes))
	}
	txType := common.Regular
	if airdrop {
		txType = common.Airdrop
	} else if premined {
		txType = common.Premined
	}
	if common.IsPreminedPeriod(s.now()) && (txType == common.Regular) {
		return nil, errors.New("provided set is not airdrop and not premined, emission has not started yet, regular transfers are forbidden")
	}
	return &txType, nil
}

func (s *Server) CheckSolanaAddress(ctx context.Context, req *CheckSolanaAddressRequest) (*CheckSolanaAddressResponse, error) {
	if err := s.solana.CheckAddress(ctx, req.SolanaAddress, req.Amount, false); err != nil {
		return nil, fmt.Errorf("Solana address check failed: %w", err)
	}
	return &CheckSolanaAddressResponse{CurrentTime: s.now()}, nil
}

func (s *Server) CheckAllowance(ctx context.Context, req *CheckAllowanceRequest) (*CheckAllowanceResponse, error) {
	var preminedAddrs []types.UnlockHash
	if req.PreminedUnlockHashes != nil {
		preminedAddrs = *req.PreminedUnlockHashes
	}
	allowance, err := s.storage.CheckAllowance(ctx, preminedAddrs)
	if err != nil {
		return nil, err
	}
	confirmationTime := s.settings.ScpTxConfirmationTime
	// Airdrop.
	airdropAllowance := AmountWithTimeEstimate{Amount: allowance.AirdropFreeCapacity, WaitEstimate: confirmationTime}
	// Premined.
	preminedAllowance := make(map[string]AmountWithTimeEstimate)
	for uh, amount := range allowance.PreminedFreeCapacity {
		preminedAllowance[uh.String()] = AmountWithTimeEstimate{Amount: amount, WaitEstimate: confirmationTime}
	}
	// Handle queue transports.
	queueFree := allowance.Queue.FreeCapacity
	queueLocked, err := s.isQueueLocked(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to check queue lock: %w", err)
	}
	if common.IsPreminedPeriod(s.now()) {
		queueFree = types.ZeroCurrency
	}
	if queueLocked {
		queueFree = types.ZeroCurrency
	}
	if queueFree.Cmp(s.settings.TransportMax) > 0 {
		queueFree = s.settings.TransportMax
	} else if queueFree.Cmp(s.settings.TransportMin) < 0 {
		queueFree = types.ZeroCurrency
	}
	regularAllowance := AmountWithTimeEstimate{Amount: queueFree}
	if !queueFree.IsZero() {
		emissionTime := s.emission.EmissionTime(allowance.Queue.QueueSize.Add(queueFree))
		regularAllowance.WaitEstimate = confirmationTime + emissionTime
	}
	return &CheckAllowanceResponse{
		Airdrop:  airdropAllowance,
		Premined: preminedAllowance,
		Regular:  regularAllowance,
	}, nil
}

func (s *Server) SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (*SubmitScpTxResponse, error) {
	if err := s.scpBlockchain.ValidTransaction(&req.Transaction); err != nil {
		return nil, fmt.Errorf("provided transaction is invalid: %w", err)
	}
	txID := req.Transaction.ID()
	confirmed, err := s.scpBlockchain.IsTxConfirmed(txID)
	if err != nil {
		return nil, fmt.Errorf("failed to check if tx is confirmed: %w", err)
	}
	inTransactionPool := s.scpBlockchain.IsInTransactionPool(txID)
	if !confirmed && !inTransactionPool {
		// TODO: check how long does it take for transporter's transactionpool to see burn tx.
		return nil, fmt.Errorf("provided transaction (%s) is not in transaction pool", txID.String())
	}
	burntAmount := common.BurntAmount(&req.Transaction)
	if burntAmount.IsZero() {
		return nil, fmt.Errorf("provided transaction (%s) does not burn any SPF", txID.String())
	}
	solanaAddr, err := s.scpBlockchain.ExtractSolanaAddress(&req.Transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to extract solana address from arbitrary data: %w", err)
	}
	if err := s.solana.CheckAddress(ctx, solanaAddr, burntAmount, true); err != nil {
		return nil, fmt.Errorf("solana address (%s) verification failed: %w", solanaAddr, err)
	}
	unlockHashes := common.ExtractSiafundUnlockHashes(&req.Transaction)
	txType, err := s.checkUnlockHashes(ctx, unlockHashes)
	if err != nil {
		return nil, fmt.Errorf("provided transaction (%s) has invalid SiafundInputs: %w", txID.String(), err)
	}
	queueLocked, err := s.isQueueLocked(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to check queue lock: %w", err)
	}
	if queueLocked && (*txType == common.Regular) {
		return nil, errors.New("queue is locked")
	}
	checkMax := (*txType == common.Regular)
	if err := s.validateAmount(burntAmount, checkMax); err != nil {
		return nil, fmt.Errorf("provided transaction (%s) burns invalid amount of SPF: %w", txID.String(), err)
	}
	var preminedAddr *types.UnlockHash
	if *txType == common.Premined {
		preminedAddr = &unlockHashes[0]
	}
	txInfo := &common.UnconfirmedTxInfo{
		BurnID:       txID,
		Amount:       burntAmount,
		SolanaAddr:   solanaAddr,
		Time:         s.now(),
		PreminedAddr: preminedAddr,
		Type:         *txType,
	}
	queueStatus, err := s.storage.AddUnconfirmedScpTx(ctx, txInfo)
	if err != nil {
		return nil, err
	}
	confirmationTime := s.settings.ScpTxConfirmationTime
	if *txType != common.Regular {
		return &SubmitScpTxResponse{WaitTimeEstimate: confirmationTime}, nil
	}
	// In case of regular transports, time needed to reach required supply is added.
	emissionTime := s.emission.EmissionTime(queueStatus.QueueSize.Add(burntAmount))
	return &SubmitScpTxResponse{
		WaitTimeEstimate: confirmationTime + emissionTime,
		SpfAmountAhead:   &queueStatus.QueueSize,
	}, nil
}

func (s *Server) TransportStatus(ctx context.Context, req *TransportStatusRequest) (*TransportStatusResponse, error) {
	record, err := s.storage.TransportRecord(ctx, req.BurnID)
	if err != nil {
		if errors.Is(err, common.ErrNotExists) {
			// Record was not found, check unconfirmed.
			_, err := s.storage.UnconfirmedInfo(ctx, req.BurnID)
			if errors.Is(err, common.ErrNotExists) {
				// Unconfirmed not found as well.
				return &TransportStatusResponse{Status: common.NotFound}, nil
			} else if err != nil {
				// Some internal database error.
				return nil, fmt.Errorf("failed to retrieve record from database: %w", err)
			}
			// Unconfirmed was found.
			return &TransportStatusResponse{Status: common.Unconfirmed}, nil
		}
		return nil, fmt.Errorf("failed to retrieve record from database: %w", err)
	}
	if record.Completed {
		return &TransportStatusResponse{Status: common.Completed}, nil
	}
	if record.SolanaTx == "" {
		return &TransportStatusResponse{Status: common.InTheQueue}, nil
	}
	return &TransportStatusResponse{Status: common.SolanaTxCreated}, nil
}

func (s *Server) History(ctx context.Context, req *HistoryRequest) (*HistoryResponse, error) {
	// TODO: implement.
	return &HistoryResponse{}, errors.New("not implemented")
}

func (s *Server) Close() error {
	s.cancel()
	s.stopWg.Wait()
	s.storage.Close()
	return s.scpBlockchain.Close()
}
