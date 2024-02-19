//go:build integration_test
// +build integration_test

package transporterdb

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

const EnvPostgresConfig = "POSTGRES_CONFIG"

func defaultFuzzer() *fuzz.Fuzzer {
	return fuzz.New().NilChance(0).NumElements(1, 150).Funcs(
		func(t *time.Time, c fuzz.Continue) {
			*t = time.Unix(c.Int63n(50257894000), 0).UTC()
		},
		func(cur *types.Currency, c fuzz.Continue) {
			*cur = types.NewCurrency64(uint64(c.Int63n(math.MaxInt64)))
		},
	)
}

func NewTestTransporterDB(t *testing.T, settings *Settings) *TransporterDB {
	postgresConfigPath := os.Getenv(EnvPostgresConfig)
	db, err := OpenPostgres(postgresConfigPath)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	tdb, err := NewDB(db, settings)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, tdb.DropSchemas(true))
		require.NoError(t, db.Close())
	})

	return tdb
}

var defaultSettings = func() *Settings {
	queueSizeLimit, err := types.NewCurrencyStr("2500000SPF")
	if err != nil {
		panic(err)
	}
	queueSizeGap := types.NewCurrency64(100000)
	return &Settings{
		PreliminaryQueueSizeLimit: queueSizeLimit.Sub(queueSizeGap),
		QueueSizeLimit:            queueSizeLimit,
	}
}()

func TestIntegrationQueueSize(t *testing.T) {
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	queueSize, err := tdb.QueueSize(ctx)
	require.NoError(t, err)
	require.Equal(t, "0", queueSize.String())
}

func TestIntegrationPreminedLimits(t *testing.T) {
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	addr2limit, err := tdb.PreminedLimits(ctx)
	require.NoError(t, err)
	require.Equal(t, map[types.UnlockHash]common.PreminedRecord{}, addr2limit)
}

func TestIntegrationConfirmedSupply(t *testing.T) {
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	supplyInfo, err := tdb.ConfirmedSupply(ctx)
	require.NoError(t, err)
	require.Equal(t, common.SupplyInfo{}, supplyInfo)
}

/*
func TestIntegrationCreateRecord(t *testing.T) {
	f := defaultFuzzer()
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	var record common.TransportRecord
	f.Fuzz(&record.TransportRequest)
	require.NoError(t, tdb.CreateRecord(ctx, &record.TransportRequest))
	gotRecord, err := tdb.Record(ctx, record.BurnID)
	require.NoError(t, err)
	require.Equal(t, record, *gotRecord)

	var solanaInfo common.SolanaTxInfo
	f.Fuzz(&solanaInfo)
	info := make(map[types.TransactionID]common.SolanaTxInfo)
	info[record.BurnID] = solanaInfo
	require.NoError(t, tdb.AddSolanaTransaction(ctx, info))
	gotRecord, err = tdb.Record(ctx, record.BurnID)
	require.NoError(t, err)
	record.SolanaTxInfo = solanaInfo
	require.Equal(t, record, *gotRecord)
}

func TestIntegrationPreminedWhitelist(t *testing.T) {
	f := defaultFuzzer()
	tdb, err := NewTestTransporterDB(t)
	require.NoError(t, err, "failed to create TransporterDB")
	ctx := context.Background()

	premined, err := tdb.PreminedWhitelist(ctx)
	require.NoError(t, err)
	require.Empty(t, premined)

	var wantPremined []common.SpfUtxo
	f.Fuzz(&wantPremined)
	t.Logf("Inserting %d premined UTXOs)", len(wantPremined))
	require.NoError(t, tdb.InsertPremined(ctx, wantPremined))
	premined, err = tdb.PreminedWhitelist(ctx)
	require.NoError(t, err)
	require.Equal(t, wantPremined, premined)

	var newPremined []common.SpfUtxo
	f.Fuzz(&newPremined)
	t.Logf("Inserting %d premined UTXOs)", len(newPremined))
	require.NoError(t, tdb.InsertPremined(ctx, newPremined))
	premined, err = tdb.PreminedWhitelist(ctx)
	require.NoError(t, err)
	wantPremined = append(wantPremined, newPremined...)
	require.Equal(t, wantPremined, premined)

	// Reload database and check.
	tdb, err = NewTestTransporterDB(t)
	require.NoError(t, err, "failed to create TransporterDB")
	premined, err = tdb.PreminedWhitelist(ctx)
	require.NoError(t, err)
	require.Equal(t, wantPremined, premined)
}

func TestIntegrationQueueMethods(t *testing.T) {
	oneHasting := types.NewCurrency64(1)
	var totalSupply types.Currency
	f := defaultFuzzer()
	f = f.Funcs(func(inv *common.SpfxInvoice, c fuzz.Continue) {
		f.Fuzz(&inv.Address)
		f.Fuzz(&inv.Amount)
		inv.TotalSupply = totalSupply
		totalSupply = totalSupply.Add(inv.Amount)
	})
	tdb, err := NewTestTransporterDB(t)
	require.NoError(t, err, "failed to create TransporterDB")
	ctx := context.Background()

	type testCase struct {
		solanaInfo common.SolanaTxInfo
		records    []common.TransportRequest
	}
	cases := make([]testCase, 100)
	f.Fuzz(&cases)
	for _, tc := range cases {
		if len(tc.records) == 0 {
			continue
		}
		// Add the records.
		for _, record := range tc.records {
			require.NoError(t, tdb.CreateRecord(ctx, &record))
		}

		// Test insufficient allowed supply.
		curSupply := tc.records[0].TotalSupply
		next, err := tdb.NextInQueue(ctx, curSupply, curSupply)
		require.NoError(t, err)
		require.Empty(t, next)
		requiredSupply := tc.records[0].SupplyAfter()
		allowedSupply := requiredSupply.Sub(oneHasting)
		next, err = tdb.NextInQueue(ctx, curSupply, allowedSupply)
		require.NoError(t, err)
		require.Empty(t, next)

		// Test in-between allowed supply.
		lastRecord := tc.records[len(tc.records)-1]
		halfAmount := lastRecord.Amount.Div64(2)
		allowedSupply = lastRecord.SupplyAfter().Sub(halfAmount)
		next, err = tdb.NextInQueue(ctx, curSupply, allowedSupply)
		require.NoError(t, err)
		require.Equal(t, tc.records[:len(tc.records)-1], next)

		// Test exceeding allowed supply.
		allowedSupply = lastRecord.SupplyAfter().Mul64(2)
		next, err = tdb.NextInQueue(ctx, curSupply, allowedSupply)
		require.NoError(t, err)
		require.Equal(t, tc.records, next)

		// Test sufficient allowed supply.
		allowedSupply = lastRecord.SupplyAfter()
		next, err = tdb.NextInQueue(ctx, curSupply, allowedSupply)
		require.NoError(t, err)
		require.Equal(t, tc.records, next)

		// Add solana info.
		info := make(map[types.TransactionID]common.SolanaTxInfo, len(tc.records))
		ids := make([]types.TransactionID, 0, len(tc.records))
		for _, record := range tc.records {
			info[record.BurnID] = tc.solanaInfo
			ids = append(ids, record.BurnID)
		}
		require.NoError(t, tdb.AddSolanaTransaction(ctx, info))

		// Commit all the records.
		require.NoError(t, tdb.Commit(ctx, ids))
		// Check that records were updated correctly.
		for _, req := range tc.records {
			gotRecord, err := tdb.Record(ctx, req.BurnID)
			require.NoError(t, err)
			wantRecord := &common.TransportRecord{
				TransportRequest: req,
				SolanaTxInfo:     tc.solanaInfo,
				Completed:        true,
			}
			require.Equal(t, wantRecord, gotRecord)
		}
	}
}
*/
