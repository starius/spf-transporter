package transporterdb

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter"
)

const (
	dropTransportRecordsTable = `
DROP TABLE IF EXISTS transport_records
`
	dropPreminedWhitelistTable = `
DROP TABLE IF EXISTS premined_whitelist
`
	dropEmissionHistoryTable = `
DROP TABLE IF EXISTS emission_history
`
	createTransportRecordsTable = `
CREATE TABLE IF NOT EXISTS transport_records (
    burn_id          text PRIMARY KEY,
    address          text NOT NULL,
    amount           bigint NOT NULL,
    total_supply     bigint UNIQUE,
    burn_time        timestamp,
    queue_up         timestamp,
    invoice_time     timestamp,
    solana_tx_time   timestamp,
    solana_tx_id     text,
    solana_confirmed boolean NOT NULL DEFAULT FALSE,
    completed        boolean NOT NULL DEFAULT FALSE
);
`
	createPreminedWhitelistTable = `
CREATE TABLE IF NOT EXISTS premined_whitelist (
    id      bigserial PRIMARY KEY,
    address text NOT NULL,
    amount  bigint NOT NULL
);
`
	createEmissionHistoryTable = `
CREATE TABLE IF NOT EXISTS emission_history (
    id             bigserial PRIMARY KEY,
    allowed_supply bigint,
    update_time    timestamp,
    tvl            bigint
);
`
	loadPreminedInterval = time.Minute * 15
)

var dropSchemas = []struct {
	query       string
	description string
}{
	{dropTransportRecordsTable, "drop transport records table"},
	{dropPreminedWhitelistTable, "drop premined whitelist table"},
	{dropEmissionHistoryTable, "drop emission history table"},
}

var createSchemas = []struct {
	query       string
	description string
}{
	{createTransportRecordsTable, "create transport records table"},
	{createPreminedWhitelistTable, "create premined whitelist table"},
	{createEmissionHistoryTable, "create emission history table"},
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

type TransporterDB struct {
	db *sql.DB

	premined   []transporter.SpfUtxo
	preminedMu sync.RWMutex

	cancel context.CancelFunc
	stopWg sync.WaitGroup // Close waits for this WaitGroup.
}

func NewDB(db *sql.DB) (*TransporterDB, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tdb := &TransporterDB{db: db, cancel: cancel}
	if err := tdb.CreateSchemas(); err != nil {
		return nil, fmt.Errorf("failed to create schemas: %w", err)
	}
	if err := tdb.loadPremined(ctx); err != nil {
		return nil, fmt.Errorf("failed to load premined: %w", err)
	}
	tdb.loadPreminedLoop(ctx)
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

func (tdb *TransporterDB) loadPremined(ctx context.Context) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: loadPremined started (%s)\n", lid)
	defer log.Printf("TransporterDB: loadPremined exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to create db objects: %w", err)
	}
	preminedRaw, err := tq.SelectWhitelisted(ctx)
	if err != nil {
		return handleErrorWithRollback(fmt.Errorf("failed to select whitelisted: %w", err), tx)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	tdb.preminedMu.Lock()
	defer tdb.preminedMu.Unlock()
	tdb.premined = tdb.premined[:0]
	for _, premined := range preminedRaw {
		value := types.NewCurrency64(uint64(premined.Amount))
		var uh types.UnlockHash
		if err := uh.LoadString(premined.Address); err != nil {
			return fmt.Errorf("failed to parse premined address: %w", err)
		}
		tdb.premined = append(tdb.premined, transporter.SpfUtxo{
			UnlockHash: uh,
			Value:      value,
		})
	}
	return nil
}

func (tdb *TransporterDB) loadPreminedLoop(ctx context.Context) {
	tdb.stopWg.Add(1)
	ticker := time.NewTicker(loadPreminedInterval)

	go func() {
		defer func() {
			ticker.Stop()
			tdb.stopWg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				log.Printf("loadPreminedLoop done by context")
				return
			case <-ticker.C:
				if err := tdb.loadPremined(ctx); err != nil {
					log.Printf("loadPremined failed: %v", err)
				}
			}
		}
	}()
}

func (tdb *TransporterDB) CreateRecord(ctx context.Context, record *transporter.TransportRequest) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: CreateRecord started (%s)\n", lid)
	defer log.Printf("TransporterDB: CreateRecord exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to create db objects: %w", err)
	}
	amount, err := record.Amount.Uint64()
	if err != nil {
		return handleErrorWithRollback(fmt.Errorf("amount is not convertable to uint64: %w", err), tx)
	}
	supply, err := record.TotalSupply.Uint64()
	if err != nil {
		return handleErrorWithRollback(fmt.Errorf("failed to convert supply to uint64: %w", err), tx)
	}
	if err := tq.CreateRecord(ctx, CreateRecordParams{
		BurnID:      record.BurnID.String(),
		Address:     string(record.Address),
		Amount:      int64(amount),
		TotalSupply: sql.NullInt64{Valid: true, Int64: int64(supply)},
		BurnTime:    sql.NullTime{Valid: true, Time: record.BurnTime},
		QueueUp:     sql.NullTime{Valid: true, Time: record.QueueUpTime},
	}); err != nil {
		return handleErrorWithRollback(err, tx)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (tdb *TransporterDB) AddSolanaTransaction(ctx context.Context, info map[types.TransactionID]transporter.SolanaTxInfo) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: AddSolanaTransaction started (%s)\n", lid)
	defer log.Printf("TransporterDB: AddSolanaTransaction exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to create db objects: %w", err)
	}

	for burnID, solanaInfo := range info {
		if err := tq.InsertSolanaInfo(ctx, InsertSolanaInfoParams{
			BurnID:       burnID.String(),
			InvoiceTime:  sql.NullTime{Valid: true, Time: solanaInfo.InvoiceTime},
			SolanaTxTime: sql.NullTime{Valid: true, Time: solanaInfo.SolanaTxTime},
			SolanaTxID:   sql.NullString{Valid: true, String: string(solanaInfo.SolanaTx)},
		}); err != nil {
			return handleErrorWithRollback(err, tx)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (tdb *TransporterDB) Commit(ctx context.Context, ids []types.TransactionID) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: Commit started (%s)\n", lid)
	defer log.Printf("TransporterDB: Commit exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to create db objects: %w", err)
	}

	for _, id := range ids {
		if err := tq.SetSolanaConfirmed(ctx, SetSolanaConfirmedParams{
			BurnID:          id.String(),
			SolanaConfirmed: true,
		}); err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to set confirmed: %w", err), tx)
		}
		if err := tq.SetCompleted(ctx, SetCompletedParams{
			BurnID:    id.String(),
			Completed: true,
		}); err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to set completed: %w", err), tx)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (tdb *TransporterDB) Record(ctx context.Context, burnID types.TransactionID) (*transporter.TransportRecord, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: Record started (%s)\n", lid)
	defer log.Printf("TransporterDB: Record exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create db objects: %w", err)
	}
	rawRecord, err := tq.SelectRecord(ctx, burnID.String())
	if err != nil {
		return nil, handleErrorWithRollback(fmt.Errorf("failed to select record: %w", err), tx)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &transporter.TransportRecord{
		TransportRequest: transporter.TransportRequest{
			SpfxInvoice: transporter.SpfxInvoice{
				Address:     transporter.SolanaAddress(rawRecord.Address),
				Amount:      types.NewCurrency64(uint64(rawRecord.Amount)),
				TotalSupply: types.NewCurrency64(uint64(rawRecord.TotalSupply.Int64)),
			},
			BurnID:      burnID,
			BurnTime:    timeFromSql(rawRecord.BurnTime),
			QueueUpTime: timeFromSql(rawRecord.QueueUp),
		},
		SolanaTxInfo: transporter.SolanaTxInfo{
			InvoiceTime:  timeFromSql(rawRecord.InvoiceTime),
			SolanaTxTime: timeFromSql(rawRecord.SolanaTxTime),
			SolanaTx:     transporter.SolanaTxID(rawRecord.SolanaTxID.String),
		},
		Completed: rawRecord.Completed,
	}, nil
}

func (tdb *TransporterDB) NextInQueue(ctx context.Context, currentSupply, allowedSupply types.Currency) ([]transporter.TransportRequest, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: NextInQueue started (%s)\n", lid)
	defer log.Printf("TransporterDB: NextInQueue exited (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create db objects: %w", err)
	}

	currentSupplyUint64, err := currentSupply.Uint64()
	if err != nil {
		return nil, handleErrorWithRollback(fmt.Errorf("current supply is not convertable to uint64: %w", err), tx)
	}
	allowedSupplyUint64, err := allowedSupply.Uint64()
	if err != nil {
		return nil, handleErrorWithRollback(fmt.Errorf("allowed supply is not convertable to uitn64: %w", err), tx)
	}
	uncompleted, err := tq.SelectUncompletedRecords(ctx, SelectUncompletedRecordsParams{
		TotalSupply: sql.NullInt64{Valid: true, Int64: int64(currentSupplyUint64)},
		Amount:      int64(allowedSupplyUint64),
	})
	if err != nil {
		return nil, handleErrorWithRollback(fmt.Errorf("failed to select uncompleted records: %w", err), tx)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	res := make([]transporter.TransportRequest, 0, len(uncompleted))
	for _, record := range uncompleted {
		res = append(res, transporter.TransportRequest{
			SpfxInvoice: transporter.SpfxInvoice{
				Address:     transporter.SolanaAddress(record.Address),
				Amount:      types.NewCurrency64(uint64(record.Amount)),
				TotalSupply: types.NewCurrency64(uint64(record.TotalSupply.Int64)),
			},
			BurnID:      parseTransactionID(record.BurnID),
			BurnTime:    timeFromSql(record.BurnTime),
			QueueUpTime: timeFromSql(record.QueueUp),
		})
	}
	return res, nil
}

func (tdb *TransporterDB) PreminedWhitelist(ctx context.Context) ([]transporter.SpfUtxo, error) {
	lid := uuid.NewString()
	log.Printf("TransporterDB: PreminedWhitelist started (%s)\n", lid)
	defer log.Printf("TransporterDB: PreminedWhitelist exited (%s)\n", lid)
	tdb.preminedMu.RLock()
	defer tdb.preminedMu.RUnlock()
	res := make([]transporter.SpfUtxo, len(tdb.premined))
	copy(res, tdb.premined)
	return res, nil
}

// InsertPremined is for tests only!
func (tdb *TransporterDB) InsertPremined(ctx context.Context, newPremined []transporter.SpfUtxo) error {
	lid := uuid.NewString()
	log.Printf("TransporterDB: InsertPremined started (%s)\n", lid)
	defer log.Printf("TransporterDB: InsertPremined exitedi (%s)\n", lid)
	tx, tq, err := tdb.createDBObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to create db objects: %w", err)
	}
	for _, premined := range newPremined {
		amount, err := premined.Value.Uint64()
		if err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to convert amount to uint64: %w", err), tx)
		}
		if err := tq.InsertWhitelisted(ctx, InsertWhitelistedParams{
			Address: premined.UnlockHash.String(),
			Amount:  int64(amount),
		}); err != nil {
			return handleErrorWithRollback(fmt.Errorf("failed to insert whitelisted: %w", err), tx)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	tdb.preminedMu.Lock()
	tdb.premined = append(tdb.premined, newPremined...)
	tdb.preminedMu.Unlock()
	return nil
}

func (tdb *TransporterDB) Close() {
	tdb.cancel()
	tdb.stopWg.Wait()
}
