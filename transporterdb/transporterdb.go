package transporterdb

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

//go:embed schema.sql
var schemaSql string

var creations = regexp.MustCompile(`CREATE[^;]+;`).FindAllString(schemaSql, -1)

func creationSql(tableName string) string {
	hits := make([]string, 0, 1)
	for _, c := range creations {
		if strings.Contains(c, tableName+" (") {
			hits = append(hits, c)
		}
	}
	if len(hits) != 1 {
		panic(fmt.Sprintf("expect exactly one hit for %s, got %d: %v", tableName, len(hits), hits))
	}
	return hits[0]
}

const (
	dropGlobalFlagsTable = `
DROP TABLE IF EXISTS global_flags
`
	dropUnconfirmedBurnsTable = `
DROP TABLE IF EXISTS unconfirmed_burns
`
	dropSolanaTransactionsTable = `
DROP TABLE IF EXISTS solana_transactions
`
	dropPreminedLimitsTable = `
DROP TABLE IF EXISTS premined_limits
`
	dropPreminedTransportsTable = `
DROP TABLE IF EXISTS premined_transports
`
	dropAirdropTransportsTable = `
DROP TABLE IF EXISTS airdrop_transports
`
	dropQueueTransportsTable = `
DROP TABLE IF EXISTS queue_transports
`

	dropTransportType = `
DROP TYPE IF EXISTS transport_type
`
)

var (
	createGlobalFlagsTable      = creationSql("global_flags")
	createUnconfirmedBurnsTable = `
DO $$
BEGIN
       IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'transport_type') THEN
               CREATE TYPE transport_type AS ENUM ('airdrop', 'premined', 'regular');
       END IF;
END$$;

` + creationSql("unconfirmed_burns")
	createSolanaTransactionsTable = creationSql("solana_transactions")
	createPreminedLimitsTable     = creationSql("premined_limits")
	createPreminedTransportsTable = creationSql("premined_transports")
	createAirdropTransportsTable  = creationSql("airdrop_transports")
	createQueueTransportsTable    = creationSql("queue_transports")
)

var dropSchemas = []struct {
	query       string
	description string
}{
	{dropGlobalFlagsTable, "drop global flags table"},
	{dropUnconfirmedBurnsTable, "drop unconfirmed burns table"},
	{dropSolanaTransactionsTable, "drop solana transactions table"},
	{dropPreminedLimitsTable, "drop premined limits table"},
	{dropPreminedTransportsTable, "drop premined transports table"},
	{dropAirdropTransportsTable, "drop airdrop transports table"},
	{dropQueueTransportsTable, "drop queue transports table"},
	{dropTransportType, "drop transport_type type"},
}

var createSchemas = []struct {
	query       string
	description string
}{
	{createGlobalFlagsTable, "create global flags table"},
	{createUnconfirmedBurnsTable, "create unconfirmed burns table"},
	{createSolanaTransactionsTable, "create solana transactions table"},
	{createPreminedLimitsTable, "create premined limits table"},
	{createPreminedTransportsTable, "create premined transports table"},
	{createAirdropTransportsTable, "create airdrop transports table"},
	{createQueueTransportsTable, "create queue transports table"},
}

// TODO: move this function to ScPrime/types.
func parseTransactionID(idStr string) (id types.TransactionID) {
	const idSize = 32
	idBytes := make([]byte, idSize)
	fmt.Sscanf(idStr, "%x", &idBytes)
	copy(id[:], idBytes)
	return
}

func handleErrorWithRollback(err error, tx *sql.Tx) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return rollbackErr
	}
	return err
}

func timeFromSql(t sql.NullTime) time.Time {
	return t.Time.UTC()
}

func transportTypeToSql(t common.TransportType) TransportType {
	switch t {
	case common.Airdrop:
		return TransportTypeAirdrop
	case common.Premined:
		return TransportTypePremined
	default:
		return TransportTypeRegular
	}
}

func transportTypeFromSql(t TransportType) common.TransportType {
	switch t {
	case TransportTypeAirdrop:
		return common.Airdrop
	case TransportTypePremined:
		return common.Premined
	default:
		return common.Regular
	}
}

type Settings struct {
	PreliminaryQueueSizeLimit types.Currency
	QueueSizeLimit            types.Currency
}

type TransporterDB struct {
	db *sql.DB

	settings *Settings

	cancel context.CancelFunc
	stopWg sync.WaitGroup // Close waits for this WaitGroup.
}

func NewDB(db *sql.DB, settings *Settings) (*TransporterDB, error) {
	_, cancel := context.WithCancel(context.Background())
	tdb := &TransporterDB{db: db, cancel: cancel, settings: settings}
	if err := tdb.CreateSchemas(); err != nil {
		return nil, fmt.Errorf("failed to create schemas: %w", err)
	}
	return tdb, nil
}

func (tdb *TransporterDB) CreateSchemas() error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: CreateSchemas started (%s)\n", lid)
	defer log.Printf("TransporterDB: CreateSchemas exited (%s)\n", lid)
	tx, err := tdb.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	for _, s := range createSchemas {
		if _, err := tx.Exec(s.query); err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to %s: %w", s.description, err), tx)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (tdb *TransporterDB) DropSchemas(cascade bool) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: DropSchemas started (%s)\n", lid)
	defer log.Printf("TransporterDB: DropSchemas exited (%s)\n", lid)
	tx, err := tdb.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	suffix := ""
	if cascade {
		suffix = " CASCADE"
	}
	for _, s := range dropSchemas {
		query := s.query + suffix
		if _, err := tx.Exec(query); err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to %s: %w", s.description, err), tx)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (tdb *TransporterDB) createDBObjects(ctx context.Context) (*sql.Tx, *Queries, error) {
	tx, err := tdb.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tq := New(tdb.db).WithTx(tx)
	return tx, tq, nil
}

type tdbMethod func(ctx context.Context, tq *Queries) error

type txCommitError struct {
	msg string
}

func (txErr txCommitError) Error() string {
	return txErr.msg
}

func (tdb *TransporterDB) runRetryableTransaction(ctx context.Context, fn tdbMethod) error {
	return retry.Do(
		func() error {
			tx, tq, err := tdb.createDBObjects(ctx)
			if err != nil {
				return fmt.Errorf("failed to create db objects: %w", err)
			}
			if err := fn(ctx, tq); err != nil {
				return handleErrorWithRollback(err, tx)
			}
			if err := tx.Commit(); err != nil {
				return txCommitError{msg: err.Error()}
			}
			return nil
		},
		retry.Context(ctx),
		retry.Delay(time.Second),
		retry.RetryIf(func(err error) bool {
			if errors.As(err, &txCommitError{}) {
				return true
			}
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code.Name() == "unique_violation" {
				return true
			}
			return false
		}),
	)
}

func preminedLimitsFromSql(premined []PreminedLimit) (map[types.UnlockHash]common.PreminedRecord, error) {
	records := make(map[types.UnlockHash]common.PreminedRecord)
	for _, pr := range premined {
		if pr.Transported >= pr.AllowedMax {
			// Skip depleted premined addresses.
			// They should be able to spend like regular.
			continue
		}
		var uh types.UnlockHash
		if err := uh.LoadString(pr.Address); err != nil {
			return nil, fmt.Errorf("failed to parse premined address: %w", err)
		}
		records[uh] = common.PreminedRecord{
			Limit:       types.NewCurrency64(uint64(pr.AllowedMax)),
			Transported: types.NewCurrency64(uint64(pr.Transported)),
		}
	}
	return records, nil
}

func (tdb *TransporterDB) PreminedLimits(ctx context.Context) (map[types.UnlockHash]common.PreminedRecord, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: PreminedLimits started (%s)\n", lid)
	defer log.Printf("TransporterDB: PreminedLimits exited (%s)\n", lid)
	records := make(map[types.UnlockHash]common.PreminedRecord)
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		preminedRaw, err := tq.SelectAllPreminedLimits(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to select all premined: %w", err)
		}
		records, err = preminedLimitsFromSql(preminedRaw)
		return err
	}); err != nil {
		return nil, err
	}
	return records, nil
}

func (tdb *TransporterDB) FindPremined(ctx context.Context, unlockHashes []types.UnlockHash) (map[types.UnlockHash]common.PreminedRecord, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: FindPremined started (%s)\n", lid)
	defer log.Printf("TransporterDB: FindPremined exited (%s)\n", lid)
	records := make(map[types.UnlockHash]common.PreminedRecord)
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		addresses := make([]string, 0, len(unlockHashes))
		for _, uh := range unlockHashes {
			addresses = append(addresses, uh.String())
		}
		preminedRaw, err := tq.SelectPremined(innerCtx, addresses)
		if err != nil {
			return fmt.Errorf("failed to select premined: %w", err)
		}
		records, err = preminedLimitsFromSql(preminedRaw)
		return err
	}); err != nil {
		return nil, err
	}
	return records, nil
}

func (tdb *TransporterDB) checkBurnIDUnique(ctx context.Context, tq *Queries, burnID types.TransactionID) error {
	burnIDInfo, err := tq.BurnIDExists(ctx, burnID.String())
	if err != nil {
		return fmt.Errorf("failed to get burnID info from db: %w", err)
	}
	if burnIDInfo.Unconfirmed {
		return fmt.Errorf("burn ID %s already exists, unconfirmed", burnID.String())
	}
	if burnIDInfo.Premined {
		return fmt.Errorf("burn ID %s already exists, premined", burnID.String())
	}
	if burnIDInfo.Airdrop {
		return fmt.Errorf("burn ID %s already exists, airdrop", burnID.String())
	}
	if burnIDInfo.Queue {
		return fmt.Errorf("burn ID %s already exists", burnID.String())
	}
	return nil
}

func (tdb *TransporterDB) unconfirmedAmount(ctx context.Context, tq *Queries, t common.TransportType) (types.Currency, error) {
	unconfirmedAmount, err := tq.UnconfirmedAmount(ctx, transportTypeToSql(t))
	if err == sql.ErrNoRows {
		return types.ZeroCurrency, nil
	} else if err != nil {
		return types.ZeroCurrency, err
	}
	var unconfirmedAmountInt64 int64
	switch a := unconfirmedAmount.(type) {
	case int64:
		unconfirmedAmountInt64 = a
	case []byte:
		unconfirmedAmountInt64, err = strconv.ParseInt(string(a), 10, 64)
		if err != nil {
			return types.ZeroCurrency, fmt.Errorf("failed to parse %q as int: %w", string(a), err)
		}
	default:
		return types.ZeroCurrency, fmt.Errorf("wrong type: %T, value: %v", unconfirmedAmount, unconfirmedAmount)
	}
	return types.NewCurrency64(uint64(unconfirmedAmountInt64)), nil
}

func emptyInterfaceToCurrency(val interface{}) (types.Currency, error) {
	num, ok := val.(int64)
	if !ok {
		return types.ZeroCurrency, fmt.Errorf("value is not convertable to int64, actual type is %T", val)
	}
	return types.NewCurrency64(uint64(num)), nil
}

func (tdb *TransporterDB) ConfirmedSupply(ctx context.Context) (inf common.SupplyInfo, err error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: ConfirmedSupply started (%s)\n", lid)
	defer log.Printf("TransporterDB: ConfirmedSupply exited (%s)\n", lid)
	if err = tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		airdropSupplyRaw, err := tq.ConfirmedAirdropSupply(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to fetch confirmed airdrop supply: %w", err)
		}
		if err == nil {
			inf.Airdrop, err = emptyInterfaceToCurrency(airdropSupplyRaw)
			if err != nil {
				return fmt.Errorf("airdrop: %w", err)
			}
		}
		preminedSupplyRaw, err := tq.ConfirmedPreminedSupply(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to fetch confirmed premined supply: %w", err)
		}
		if err == nil {
			inf.Premined, err = emptyInterfaceToCurrency(preminedSupplyRaw)
			if err != nil {
				return fmt.Errorf("premined: %w", err)
			}
		}
		regularSupplyRaw, err := tq.CompletedQueueSupply(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to fetch confirmed queue supply: %w", err)
		}
		if err == nil {
			inf.Regular, err = emptyInterfaceToCurrency(regularSupplyRaw)
			if err != nil {
				return fmt.Errorf("regular: %w", err)
			}
		}
		return nil
	}); err != nil {
		return common.SupplyInfo{}, err
	}
	return inf, nil
}

func (tdb *TransporterDB) supplyAfterTransports(ctx context.Context, tq *Queries, t common.TransportType) (types.Currency, error) {
	var supply interface{}
	var err error
	switch t {
	case common.Airdrop:
		supply, err = tq.AirdropSupply(ctx)
		if err != nil && err != sql.ErrNoRows {
			return types.ZeroCurrency, fmt.Errorf("failed to fetch airdrop supply: %w", err)
		}
	case common.Premined:
		supply, err = tq.PreminedSupply(ctx)
		if err != nil && err != sql.ErrNoRows {
			return types.ZeroCurrency, fmt.Errorf("failed to fetch premined supply: %w", err)
		}
	case common.Regular:
		supply, err = tq.UncompletedQueueSupply(ctx)
		if err != nil && err != sql.ErrNoRows {
			return types.ZeroCurrency, fmt.Errorf("failed to fetch queue supply: %w", err)
		}
	}
	if err == sql.ErrNoRows {
		return types.ZeroCurrency, nil
	}
	return emptyInterfaceToCurrency(supply)
}

type queueState struct {
	completedSupply   types.Currency
	uncompletedSupply types.Currency
}

func (qs *queueState) size() types.Currency {
	return qs.uncompletedSupply.Sub(qs.completedSupply)
}

func (tdb *TransporterDB) queueState(ctx context.Context, tq *Queries) (*queueState, error) {
	var completedSupply, uncompletedSupply int64
	var ok bool
	completedSupplyRaw, err := tq.CompletedQueueSupply(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch completed queue supply: %w", err)
	}
	if err == nil {
		completedSupply, ok = completedSupplyRaw.(int64)
		if !ok {
			return nil, errors.New("completed supply is not convertable to int64")
		}
	}
	uncompletedSupplyRaw, err := tq.UncompletedQueueSupply(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch uncompleted queue supply: %w", err)
	}
	if err == nil {
		uncompletedSupply, ok = uncompletedSupplyRaw.(int64)
		if !ok {
			return nil, errors.New("uncompleted supply is not convertable to int64")
		}
	}
	return &queueState{
		completedSupply:   types.NewCurrency64(uint64(completedSupply)),
		uncompletedSupply: types.NewCurrency64(uint64(uncompletedSupply)),
	}, nil
}

func (tdb *TransporterDB) QueueSize(ctx context.Context) (queueSize types.Currency, err error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: QueueSize started (%s)\n", lid)
	defer log.Printf("TransporterDB: QueueSize exited (%s)\n", lid)
	if err = tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		qs, err := tdb.queueState(innerCtx, tq)
		if err != nil {
			return fmt.Errorf("queue state: %w", err)
		}
		unconfirmedAmount, err := tdb.unconfirmedAmount(innerCtx, tq, common.Regular)
		if err != nil {
			return fmt.Errorf("failed to fetch unconfirmed amount: %w", err)
		}
		queueSize = qs.size().Add(unconfirmedAmount)
		return nil
	}); err != nil {
		return types.ZeroCurrency, nil
	}
	return queueSize, nil
}

func (tdb *TransporterDB) airdropAllowance(ctx context.Context, tq *Queries) (types.Currency, error) {
	unconfirmedAmount, err := tdb.unconfirmedAmount(ctx, tq, common.Airdrop)
	if err != nil {
		return types.ZeroCurrency, fmt.Errorf("failed to fetch unconfirmed amount: %w", err)
	}
	airdropSupply, err := tdb.supplyAfterTransports(ctx, tq, common.Airdrop)
	if err != nil {
		return types.ZeroCurrency, fmt.Errorf("airdrop supply: %w", err)
	}
	unconfirmedAirdropSupply := airdropSupply.Add(unconfirmedAmount)
	return common.AirdropMax.Sub(unconfirmedAirdropSupply), nil
}

func (tdb *TransporterDB) preminedAllowance(ctx context.Context, tq *Queries, unlockHashes []types.UnlockHash) (map[types.UnlockHash]types.Currency, error) {
	unlockHashesStr := make([]string, 0, len(unlockHashes))
	for _, uh := range unlockHashes {
		unlockHashesStr = append(unlockHashesStr, uh.String())
	}
	premined, err := tq.SelectPremined(ctx, unlockHashesStr)
	if err != nil {
		return nil, fmt.Errorf("failed to select premined: %w", err)
	}
	if len(premined) != len(unlockHashes) {
		return nil, fmt.Errorf("not all of provided unlock hashes are premined: got %d premined, total %d", len(premined), len(unlockHashes))
	}
	allowance := make(map[types.UnlockHash]types.Currency)
	for _, pr := range premined {
		var uh types.UnlockHash
		if err := uh.LoadString(pr.Address); err != nil {
			return nil, fmt.Errorf("failed to parse premined address: %w", err)
		}
		transported := types.NewCurrency64(uint64(pr.Transported))
		allowedMax := types.NewCurrency64(uint64(pr.AllowedMax))
		allowance[uh] = allowedMax.Sub(transported)
	}
	return allowance, nil
}

func (tdb *TransporterDB) queueAllowance(ctx context.Context, tq *Queries, queueSizeLimit types.Currency) (*common.QueueAllowance, error) {
	unconfirmedAmount, err := tdb.unconfirmedAmount(ctx, tq, common.Regular)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch unconfirmed amount: %w", err)
	}
	queueState, err := tdb.queueState(ctx, tq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch queue state: %w", err)
	}
	queueSize := queueState.size().Add(unconfirmedAmount)
	queueEndSupply := queueState.completedSupply.Add(queueSize)
	supplyLeft := common.TotalSupply.Sub(queueEndSupply)
	queueCapacityLeft := queueSizeLimit.Sub(queueSize)
	var freeCapacity types.Currency
	if supplyLeft.Cmp(queueCapacityLeft) >= 0 {
		freeCapacity = queueCapacityLeft
	} else {
		freeCapacity = supplyLeft
	}
	return &common.QueueAllowance{
		FreeCapacity: freeCapacity,
		QueueSize:    queueSize,
	}, nil
}

func (tdb *TransporterDB) CheckAllowance(ctx context.Context, preminedAddrs []types.UnlockHash) (*common.Allowance, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: CheckAllowance started (%s)\n", lid)
	defer log.Printf("TransporterDB: CheckAllowance exited (%s)\n", lid)
	qa := &common.Allowance{}
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		queueAllowance, err := tdb.queueAllowance(innerCtx, tq, tdb.settings.PreliminaryQueueSizeLimit)
		if err != nil {
			return fmt.Errorf("failed to fetch queue allowance: %w", err)
		}
		qa.Queue = *queueAllowance
		airdropAllowance, err := tdb.airdropAllowance(innerCtx, tq)
		if err != nil {
			return fmt.Errorf("failed to fetch airdrop allowance: %w", err)
		}
		qa.AirdropFreeCapacity = airdropAllowance
		if len(preminedAddrs) != 0 {
			preminedAllowance, err := tdb.preminedAllowance(innerCtx, tq, preminedAddrs)
			if err != nil {
				return fmt.Errorf("failed to fetch premined allowance: %w", err)
			}
			qa.PreminedFreeCapacity = preminedAllowance
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return qa, nil
}

func (tdb *TransporterDB) AddUnconfirmedScpTx(ctx context.Context, info *common.UnconfirmedTxInfo) (s *common.QueueAllowance, err error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: AddUnconfirmedScpTx started (%s)\n", lid)
	defer log.Printf("TransporterDB: AddUnconfirmedScpTx exited (%s)\n", lid)
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		// Check if burn ID is unique.
		if err := tdb.checkBurnIDUnique(innerCtx, tq, info.BurnID); err != nil {
			return err
		}

		if info.Amount.IsZero() {
			return errors.New("zero amounts are not allowed")
		}

		// Check the limits (airdrop, premined, queue size).
		var allowance types.Currency
		switch info.Type {
		case common.Airdrop:
			allowance, err = tdb.airdropAllowance(innerCtx, tq)
			if err != nil {
				return fmt.Errorf("failed to fetch airdrop allowance: %w", err)
			}
		case common.Premined:
			allowanceMap, err := tdb.preminedAllowance(innerCtx, tq, []types.UnlockHash{*info.PreminedAddr})
			if err != nil {
				return fmt.Errorf("failed to fetch premined allowance: %w", err)
			}
			allowance = allowanceMap[*info.PreminedAddr]
		case common.Regular:
			queueAllowance, err := tdb.queueAllowance(innerCtx, tq, tdb.settings.QueueSizeLimit)
			if err != nil {
				return fmt.Errorf("failed to fetch queue allowance: %w", err)
			}
			allowance = queueAllowance.FreeCapacity
			s = queueAllowance
		}
		if allowance.Cmp(info.Amount) < 0 {
			return fmt.Errorf("allowance is exceeded: amount %s > allowed max %s", info.Amount.String(), allowance.String())
		}

		if info.Type == common.Premined {
			// Increase premined amount transported by this unlock hash.
			if err := tq.IncreasePreminedTransported(innerCtx, IncreasePreminedTransportedParams{
				Address:     info.PreminedAddr.String(),
				Transported: info.Amount.Big().Int64(),
			}); err != nil {
				return fmt.Errorf("failed to increase transported amount: %w", err)
			}
		}

		// Insert new unconfirmed transaction.
		preminedAddress := sql.NullString{}
		if info.PreminedAddr != nil {
			preminedAddress.Valid = true
			preminedAddress.String = info.PreminedAddr.String()
		}
		if err := tq.InsertUnconfirmed(innerCtx, InsertUnconfirmedParams{
			BurnID:          info.BurnID.String(),
			Amount:          info.Amount.Big().Int64(),
			SolanaAddress:   info.SolanaAddr.String(),
			PreminedAddress: preminedAddress,
			Time:            sql.NullTime{Valid: true, Time: info.Time},
			TxType:          transportTypeToSql(info.Type),
		}); err != nil {
			return fmt.Errorf("failed to insert unconfirmed: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

func unconfirmedTxInfoFromSql(inf UnconfirmedBurn) (*common.UnconfirmedTxInfo, error) {
	var preminedAddr *types.UnlockHash
	if inf.PreminedAddress.Valid {
		preminedAddr = &types.UnlockHash{}
		if err := preminedAddr.LoadString(inf.PreminedAddress.String); err != nil {
			return nil, fmt.Errorf("failed to parse premined address: %w", err)
		}
	}
	solanaAddr, err := common.SolanaAddressFromString(inf.SolanaAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Solana addresss: %w", err)
	}
	utx := &common.UnconfirmedTxInfo{
		BurnID:       parseTransactionID(inf.BurnID),
		Amount:       types.NewCurrency64(uint64(inf.Amount)),
		SolanaAddr:   solanaAddr,
		PreminedAddr: preminedAddr,
		Time:         timeFromSql(inf.Time),
		Type:         transportTypeFromSql(inf.TxType),
	}
	if inf.Height.Valid {
		height := types.BlockHeight(uint64(inf.Height.Int64))
		utx.Height = &height
	}
	return utx, nil
}

func (tdb *TransporterDB) UnconfirmedBefore(ctx context.Context, before time.Time) ([]common.UnconfirmedTxInfo, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: UnconfirmedBefore started (%s)\n", lid)
	defer log.Printf("TransporterDB: UnconfirmedBefore exited (%s)\n", lid)
	var unconfirmed []common.UnconfirmedTxInfo
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		unconfirmedRaw, err := tq.SelectUnconfirmed(innerCtx, sql.NullTime{Valid: true, Time: before})
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to select unconfirmed: %w", err)
		}
		for _, ur := range unconfirmedRaw {
			inf, err := unconfirmedTxInfoFromSql(ur)
			if err != nil {
				return fmt.Errorf("unconfirmed tx info from sql: %w", err)
			}
			unconfirmed = append(unconfirmed, *inf)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return unconfirmed, nil
}

func (tdb *TransporterDB) insertTransport(ctx context.Context, tq *Queries, tx common.UnconfirmedTxInfo, prevSupply types.Currency, curTime time.Time) error {
	newSupply := prevSupply.Add(tx.Amount)

	supplyBefore := sql.NullInt64{
		Valid: true,
		Int64: prevSupply.Big().Int64(),
	}
	if supplyBefore.Int64 == 0 {
		supplyBefore.Valid = false
	}

	switch tx.Type {
	case common.Airdrop:
		if err := tq.InsertToAirdrop(ctx, InsertToAirdropParams{
			BurnID:        tx.BurnID.String(),
			SolanaAddress: tx.SolanaAddr.String(),
			SupplyAfter:   newSupply.Big().Int64(),
			SupplyBefore:  supplyBefore,
			BurnTime:      sql.NullTime{Valid: true, Time: tx.Time},
		}); err != nil {
			return fmt.Errorf("failed to insert airdrop: %w", err)
		}
	case common.Premined:
		if err := tq.InsertToPremined(ctx, InsertToPreminedParams{
			BurnID:        tx.BurnID.String(),
			Address:       tx.PreminedAddr.String(),
			SupplyAfter:   newSupply.Big().Int64(),
			SupplyBefore:  supplyBefore,
			BurnTime:      sql.NullTime{Valid: true, Time: tx.Time},
			SolanaAddress: tx.SolanaAddr.String(),
		}); err != nil {
			return fmt.Errorf("failed to insert premined: %w", err)
		}
	case common.Regular:
		if err := tq.InsertToQueue(ctx, InsertToQueueParams{
			BurnID:        tx.BurnID.String(),
			SolanaAddress: tx.SolanaAddr.String(),
			SupplyAfter:   newSupply.Big().Int64(),
			SupplyBefore:  supplyBefore,
			BurnTime:      sql.NullTime{Valid: true, Time: tx.Time},
			QueueUp:       sql.NullTime{Valid: true, Time: curTime},
		}); err != nil {
			return fmt.Errorf("failed to insert into queue: %w", err)
		}
	}
	return nil
}

func transportRequestFromUnconfirmed(utx common.UnconfirmedTxInfo, prevSupply types.Currency) common.TransportRequest {
	return common.TransportRequest{
		SpfxInvoice: common.SpfxInvoice{
			Address:     utx.SolanaAddr,
			Amount:      utx.Amount,
			TotalSupply: prevSupply,
		},
		BurnID:   utx.BurnID,
		BurnTime: utx.Time,
		Type:     utx.Type,
	}
}

func (tdb *TransporterDB) ConfirmUnconfirmed(ctx context.Context, txs []common.UnconfirmedTxInfo, curTime time.Time) ([]common.TransportRequest, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: ConfirmUnconfirmed started (%s)\n", lid)
	defer log.Printf("TransporterDB: ConfirmUnconfirmed exited (%s)\n", lid)
	reqs := make([]common.TransportRequest, 0, len(txs))
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		ids := make([]string, 0, len(txs))
		supplyMap := make(map[common.TransportType]types.Currency)
		for _, tx := range txs {
			ids = append(ids, tx.BurnID.String())
			supply, ok := supplyMap[tx.Type]
			if !ok {
				var err error
				supply, err = tdb.supplyAfterTransports(innerCtx, tq, tx.Type)
				if err != nil {
					return fmt.Errorf("failed to fetch supply: %w", err)
				}
			}
			if err := tdb.insertTransport(innerCtx, tq, tx, supply, curTime); err != nil {
				return fmt.Errorf("failed to insert transport: %w", err)
			}
			reqs = append(reqs, transportRequestFromUnconfirmed(tx, supply))
			supplyMap[tx.Type] = supply.Add(tx.Amount)
		}
		if err := tq.RemoveUnconfirmed(innerCtx, ids); err != nil {
			return fmt.Errorf("failed to remove unconfirmed: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reqs, nil
}

func (tdb *TransporterDB) SetConfirmationHeight(ctx context.Context, ids []types.TransactionID, height types.BlockHeight) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: SetConfirmationHeight started (%s)\n", lid)
	defer log.Printf("TransporterDB: SetConfirmationHeight exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		for _, id := range ids {
			if err := tq.SetUnconfirmedBurnHeight(innerCtx, SetUnconfirmedBurnHeightParams{
				Height: sql.NullInt64{Valid: true, Int64: int64(height)},
				BurnID: id.String(),
			}); err != nil {
				return fmt.Errorf("failed to set unconfirmed burn height: %w", err)
			}
		}
		return nil
	})
}

func (tdb *TransporterDB) RemoveUnconfirmed(ctx context.Context, txs []common.UnconfirmedTxInfo) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: RemoveUnconfirmed started (%s)\n", lid)
	defer log.Printf("TransporterDB: RemoveUnconfirmed exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		ids := make([]string, 0, len(txs))
		for _, tx := range txs {
			ids = append(ids, tx.BurnID.String())
			if tx.Type != common.Premined {
				continue
			}
			if err := tq.DecreasePremined(innerCtx, DecreasePreminedParams{
				Address:     tx.PreminedAddr.String(),
				Transported: tx.Amount.Big().Int64(),
			}); err != nil {
				return fmt.Errorf("failed to decrease premined: %w", err)
			}
		}
		if err := tq.RemoveUnconfirmed(innerCtx, ids); err != nil {
			return fmt.Errorf("failed to remove unconfirmed: %w", err)
		}
		return nil
	})
}

func (tdb *TransporterDB) RemoveSolanaTransaction(ctx context.Context, burnID types.TransactionID, t common.TransportType) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: RemoveSolanaTransaction started (%s)\n", lid)
	defer log.Printf("TransporterDB: RemoveSolanaTransaction exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		switch t {
		case common.Premined:
			if err := tq.RemoveSolanaTransactionFromPremined(innerCtx, burnID.String()); err != nil {
				return fmt.Errorf("failed to remove solana tx from premined table: %w", err)
			}
		case common.Airdrop:
			if err := tq.RemoveSolanaTransactionFromAirdrop(innerCtx, burnID.String()); err != nil {
				return fmt.Errorf("failed to remove solana tx from airdrop table: %w", err)
			}
		case common.Regular:
			if err := tq.RemoveSolanaTransactionFromQueue(innerCtx, burnID.String()); err != nil {
				return fmt.Errorf("failed to remove solana tx from queue table: %w", err)
			}
		}
		return nil
	})
}

func (tdb *TransporterDB) AddSolanaTransaction(ctx context.Context, burnID types.TransactionID, t common.TransportType, info common.SolanaTxInfo) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: AddSolanaTransaction started (%s)\n", lid)
	defer log.Printf("TransporterDB: AddSolanaTransaction exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		if err := tq.InsertSolanaTransaction(innerCtx, InsertSolanaTransactionParams{
			ID:            string(info.SolanaTx),
			BroadcastTime: info.BroadcastTime,
		}); err != nil {
			return fmt.Errorf("failed to insert solana transaction: %w", err)
		}
		switch t {
		case common.Premined:
			if err := tq.InsertSolanaTransactionToPremined(innerCtx, InsertSolanaTransactionToPreminedParams{
				BurnID:   burnID.String(),
				SolanaID: sql.NullString{Valid: true, String: string(info.SolanaTx)},
			}); err != nil {
				return fmt.Errorf("failed to insert solana tx to premined table: %w", err)
			}
		case common.Airdrop:
			if err := tq.InsertSolanaTransactionToAirdrop(innerCtx, InsertSolanaTransactionToAirdropParams{
				BurnID:   burnID.String(),
				SolanaID: sql.NullString{Valid: true, String: string(info.SolanaTx)},
			}); err != nil {
				return fmt.Errorf("failed to insert solana tx to airdrop table: %w", err)
			}
		case common.Regular:
			if err := tq.InsertSolanaTransactionToQueue(innerCtx, InsertSolanaTransactionToQueueParams{
				BurnID:   burnID.String(),
				SolanaID: sql.NullString{Valid: true, String: string(info.SolanaTx)},
			}); err != nil {
				return fmt.Errorf("failed to insert solana tx to queue table: %w", err)
			}
		}
		return nil
	})
}

func (tdb *TransporterDB) ConfirmSolana(ctx context.Context, solanaID common.SolanaTxID, confirmTime time.Time) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: ConfirmSolana started (%s)\n", lid)
	defer log.Printf("TransporterDB: ConfirmSolana exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		if err := tq.ConfirmSolanaTransaction(innerCtx, ConfirmSolanaTransactionParams{
			ID:               string(solanaID),
			ConfirmationTime: sql.NullTime{Valid: true, Time: confirmTime},
		}); err != nil {
			return fmt.Errorf("failed to mark solana tx confirmed: %w", err)
		}
		return nil
	})
}

func (tdb *TransporterDB) UnconfirmedInfo(ctx context.Context, burnID types.TransactionID) (*common.UnconfirmedTxInfo, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: UnconfirmedInfo started (%s)\n", lid)
	defer log.Printf("TransporterDB: UnconfirmeInfo exited (%s)\n", lid)
	record := &common.UnconfirmedTxInfo{}
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		rawRecord, err := tq.SelectUnconfirmedRecord(innerCtx, burnID.String())
		if err == sql.ErrNoRows {
			return common.ErrNotExists
		} else if err != nil {
			return fmt.Errorf("failed to select unconfirmed: %w", err)
		}
		record, err = unconfirmedTxInfoFromSql(rawRecord)
		if err != nil {
			return fmt.Errorf("unconfirmed tx info from sql: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return record, nil
}

func transportRequestFromSql(qt QueueTransport) (common.TransportRequest, error) {
	amount := uint64(qt.SupplyAfter - qt.SupplyBefore.Int64)
	solanaAddr, err := common.SolanaAddressFromString(qt.SolanaAddress)
	if err != nil {
		return common.TransportRequest{}, err
	}
	queueUpTime := timeFromSql(qt.QueueUp)
	return common.TransportRequest{
		SpfxInvoice: common.SpfxInvoice{
			Address:     solanaAddr,
			Amount:      types.NewCurrency64(amount),
			TotalSupply: types.NewCurrency64(uint64(qt.SupplyBefore.Int64)),
		},
		BurnID:      parseTransactionID(qt.BurnID),
		BurnTime:    timeFromSql(qt.BurnTime),
		QueueUpTime: &queueUpTime,
		Type:        common.Regular,
	}, nil
}

func (tdb *TransporterDB) transportRecord(ctx context.Context, tq *Queries, burnID types.TransactionID) (*common.TransportRecord, error) {
	t, err := tq.BurnIDExists(ctx, burnID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to check burn ID: %w", err)
	}
	solanaID := ""
	var req common.TransportRequest
	if t.Premined {
		rawRecord, err := tq.SelectPreminedRecord(ctx, burnID.String())
		if err != nil {
			return nil, fmt.Errorf("select premined: %w", err)
		}
		solanaAddr, err := common.SolanaAddressFromString(rawRecord.SolanaAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse solana address (premined) %q: %w", rawRecord.SolanaAddress, err)
		}
		amount := uint64(rawRecord.SupplyAfter - rawRecord.SupplyBefore.Int64)
		solanaID = rawRecord.SolanaID.String
		req = common.TransportRequest{
			SpfxInvoice: common.SpfxInvoice{
				Address:     solanaAddr,
				Amount:      types.NewCurrency64(amount),
				TotalSupply: types.NewCurrency64(uint64(rawRecord.SupplyBefore.Int64)),
			},
			BurnID:   burnID,
			BurnTime: timeFromSql(rawRecord.BurnTime),
			Type:     common.Premined,
		}
	} else if t.Airdrop {
		rawRecord, err := tq.SelectAirdropRecord(ctx, burnID.String())
		if err != nil {
			return nil, fmt.Errorf("select airdrop: %w", err)
		}
		solanaAddr, err := common.SolanaAddressFromString(rawRecord.SolanaAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse solana address (airdrop) %q: %w", rawRecord.SolanaAddress, err)
		}
		amount := uint64(rawRecord.SupplyAfter - rawRecord.SupplyBefore.Int64)
		solanaID = rawRecord.SolanaID.String
		req = common.TransportRequest{
			SpfxInvoice: common.SpfxInvoice{
				Address:     solanaAddr,
				Amount:      types.NewCurrency64(amount),
				TotalSupply: types.NewCurrency64(uint64(rawRecord.SupplyBefore.Int64)),
			},
			BurnID:   burnID,
			BurnTime: timeFromSql(rawRecord.BurnTime),
			Type:     common.Airdrop,
		}
	} else if t.Queue {
		rawRecord, err := tq.SelectQueueRecord(ctx, burnID.String())
		if err != nil {
			return nil, fmt.Errorf("select queue record: %w", err)
		}
		solanaID = rawRecord.SolanaID.String
		req, err = transportRequestFromSql(rawRecord)
		if err != nil {
			return nil, fmt.Errorf("failed to parse transport request: %w", err)
		}
	} else {
		return nil, common.ErrNotExists
	}
	record := &common.TransportRecord{TransportRequest: req}
	if solanaID != "" {
		solanaInfo, err := tq.SelectSolanaTransaction(ctx, solanaID)
		if err != nil {
			return nil, fmt.Errorf("failed to select solana transaction: %w", err)
		}
		record.SolanaTxInfo = common.SolanaTxInfo{
			BroadcastTime: solanaInfo.BroadcastTime.UTC(),
			SolanaTx:      common.SolanaTxID(solanaID),
		}
		record.ConfirmationTime = timeFromSql(solanaInfo.ConfirmationTime)
		record.Completed = solanaInfo.Confirmed
	}
	return record, nil
}

func (tdb *TransporterDB) TransportRecord(ctx context.Context, burnID types.TransactionID) (*common.TransportRecord, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: TransportRecord started (%s)\n", lid)
	defer log.Printf("TransporterDB: TransportRecord exited (%s)\n", lid)
	record := &common.TransportRecord{}
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		var err error
		record, err = tdb.transportRecord(innerCtx, tq, burnID)
		if err != nil {
			return fmt.Errorf("failed to fetch transport request: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return record, nil
}

func (tdb *TransporterDB) UncompletedPremined(ctx context.Context) ([]common.TransportRequest, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: UncompletedPremined started (%s)\n", lid)
	defer log.Printf("TransporterDB: UncompletedPremined exited (%s)\n", lid)
	var res []common.TransportRequest
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		uncompleted, err := tq.SelectUncompletedPremined(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to select uncompleted from premined: %w", err)
		}
		for _, record := range uncompleted {
			amount := uint64(record.SupplyAfter - record.SupplyBefore.Int64)
			solanaAddr, err := common.SolanaAddressFromString(record.SolanaAddress)
			if err != nil {
				return fmt.Errorf("failed to parse solana address (UncompletedPremined) %q: %w", record.SolanaAddress, err)
			}
			res = append(res, common.TransportRequest{
				SpfxInvoice: common.SpfxInvoice{
					Address:     solanaAddr,
					Amount:      types.NewCurrency64(amount),
					TotalSupply: types.NewCurrency64(uint64(record.SupplyBefore.Int64)),
				},
				BurnID:   parseTransactionID(record.BurnID),
				BurnTime: timeFromSql(record.BurnTime),
				Type:     common.Premined,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func (tdb *TransporterDB) UncompletedAirdrop(ctx context.Context) ([]common.TransportRequest, error) {

	lid := uuid.NewString()
	log.Printf("TransporterDB: UncompletedAirdrop started (%s)\n", lid)
	defer log.Printf("TransporterDB: UncompletedAirdrop exited (%s)\n", lid)
	var res []common.TransportRequest
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		uncompleted, err := tq.SelectUncompletedAirdrop(innerCtx)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to select uncompleted from airdrop: %w", err)
		}
		for _, record := range uncompleted {
			amount := uint64(record.SupplyAfter - record.SupplyBefore.Int64)
			solanaAddr, err := common.SolanaAddressFromString(record.SolanaAddress)
			if err != nil {
				return fmt.Errorf("failed to parse solana address (UncompletedAirdrop) %q: %w", record.SolanaAddress, err)
			}
			res = append(res, common.TransportRequest{
				SpfxInvoice: common.SpfxInvoice{
					Address:     solanaAddr,
					Amount:      types.NewCurrency64(amount),
					TotalSupply: types.NewCurrency64(uint64(record.SupplyBefore.Int64)),
				},
				BurnID:   parseTransactionID(record.BurnID),
				BurnTime: timeFromSql(record.BurnTime),
				Type:     common.Airdrop,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func (tdb *TransporterDB) NextInQueue(ctx context.Context, allowedSupply types.Currency) ([]common.TransportRequest, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: NextInQueue started (%s)\n", lid)
	defer log.Printf("TransporterDB: NextInQueue exited (%s)\n", lid)
	var res []common.TransportRequest
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		uncompleted, err := tq.SelectUncompletedFromQueue(innerCtx, allowedSupply.Big().Int64())
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to select uncompleted from queue: %w", err)
		}
		for _, record := range uncompleted {
			amount := uint64(record.SupplyAfter - record.SupplyBefore.Int64)
			queueUpTime := timeFromSql(record.QueueUp)
			solanaAddr, err := common.SolanaAddressFromString(record.SolanaAddress)
			if err != nil {
				return fmt.Errorf("failed to parse solana address (NextInQueue) %q: %w", record.SolanaAddress, err)
			}
			res = append(res, common.TransportRequest{
				SpfxInvoice: common.SpfxInvoice{
					Address:     solanaAddr,
					Amount:      types.NewCurrency64(amount),
					TotalSupply: types.NewCurrency64(uint64(record.SupplyBefore.Int64)),
				},
				BurnID:      parseTransactionID(record.BurnID),
				BurnTime:    timeFromSql(record.BurnTime),
				QueueUpTime: &queueUpTime,
				Type:        common.Regular,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func (tdb *TransporterDB) RecordsWithUnconfirmedSolana(ctx context.Context) ([]common.TransportRecord, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: RecordsWithUnconfirmedSolana started (%s)\n", lid)
	defer log.Printf("TransporterDB: RecordsWithUnconfirmedSolana exited (%s)\n", lid)
	var records []common.TransportRecord
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		unconfirmed, err := tq.SelectSolanaUnconfirmed(innerCtx)
		if err == sql.ErrNoRows {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to select solana unconfirmed: %w", err)
		}
		for _, id := range unconfirmed {
			record, err := tdb.transportRecord(innerCtx, tq, parseTransactionID(id))
			if err != nil {
				return fmt.Errorf("failed to select record %s: %w", id, err)
			}
			records = append(records, *record)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return records, nil
}

// InsertPremined is for tests only!
func (tdb *TransporterDB) InsertPremined(ctx context.Context, newPremined []common.SpfAddressBalance) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: InsertPremined started (%s)\n", lid)
	defer log.Printf("TransporterDB: InsertPremined exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		for _, addressBalance := range newPremined {
			if err := tq.InsertPremined(innerCtx, InsertPreminedParams{
				Address:    addressBalance.UnlockHash.String(),
				AllowedMax: addressBalance.Value.Big().Int64(),
			}); err != nil {
				return fmt.Errorf("failed to insert premined: %w", err)
			}
		}
		return nil
	})
}

func (tdb *TransporterDB) SetFlag(ctx context.Context, name string, value bool) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: SetFlag started (%s)\n", lid)
	defer log.Printf("TransporterDB: SetFlag exited (%s)\n", lid)
	return tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		_, err := tq.ReadFlag(innerCtx, name)
		if err == sql.ErrNoRows {
			if err := tq.InsertFlag(innerCtx, InsertFlagParams{
				Name:  name,
				Value: sql.NullBool{Valid: true, Bool: value},
			}); err != nil {
				return fmt.Errorf("failed to insert flag %s: %w", name, err)
			}
		} else if err != nil {
			return fmt.Errorf("read flag %s: %w", name, err)
		} else {
			if err := tq.UpdateFlag(innerCtx, UpdateFlagParams{
				Name:  name,
				Value: sql.NullBool{Valid: true, Bool: value},
			}); err != nil {
				return fmt.Errorf("failed to update flag %s: %w", name, err)
			}
		}
		return nil
	})
}

func (tdb *TransporterDB) GetFlag(ctx context.Context, name string) (bool, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: GetFlag started (%s)\n", lid)
	defer log.Printf("TransporterDB: GetFlag exited (%s)\n", lid)
	var val bool
	if err := tdb.runRetryableTransaction(ctx, func(innerCtx context.Context, tq *Queries) error {
		valRaw, err := tq.ReadFlag(innerCtx, name)
		// Missing flags default to false, no error is returned.
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("read flag: %w", err)
		}
		val = valRaw.Bool
		return nil
	}); err != nil {
		return false, err
	}
	return val, nil
}

func (tdb *TransporterDB) Close() {
	tdb.cancel()
	tdb.stopWg.Wait()
}
