//go:build integration_test
// +build integration_test

package transporterdb

import (
	"context"
	"os"
	"sync"
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
			*cur = types.NewCurrency64(uint64(c.Intn(1000)))
		},
		func(tt *common.TransportType, c fuzz.Continue) {
			*tt = common.TransportType(c.Intn(int(common.TransportTypeCount)))
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

func TestIntegrationRunRetryableTransaction(t *testing.T) {
	f := defaultFuzzer()
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	var infos []*common.UnconfirmedTxInfo
	for i := 0; i < 10; i++ {
		info := &common.UnconfirmedTxInfo{}
		f.Fuzz(info)
		info.PreminedAddr = nil
		info.Type = common.Regular
		_, err := tdb.AddUnconfirmedScpTx(ctx, info)
		require.NoError(t, err)
		infos = append(infos, info)
	}

	var wg sync.WaitGroup
	wg.Add(len(infos))
	now := time.Now()
	for _, info := range infos {
		info := info
		go func() {
			defer wg.Done()
			_, err := tdb.ConfirmUnconfirmed(ctx, []common.UnconfirmedTxInfo{*info}, now)
			require.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestIntegrationQueueNotEmpty(t *testing.T) {
	f := defaultFuzzer()
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	queueSize, err := tdb.QueueSize(ctx)
	require.NoError(t, err)
	require.Equal(t, "0", queueSize.String())

	var infos []*common.UnconfirmedTxInfo
	var sum types.Currency
	for i := 0; i < 10; i++ {
		info := &common.UnconfirmedTxInfo{}
		f.Fuzz(info)
		info.PreminedAddr = nil
		info.Type = common.Regular
		_, err := tdb.AddUnconfirmedScpTx(ctx, info)
		require.NoError(t, err)
		infos = append(infos, info)
		sum = sum.Add(info.Amount)
	}

	queueSize, err = tdb.QueueSize(ctx)
	require.NoError(t, err)
	require.Equal(t, sum.String(), queueSize.String())
}

func TestIntegrationPremined(t *testing.T) {
	f := defaultFuzzer()
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	t.Run("empty premined limits", func(t *testing.T) {
		addr2limit, err := tdb.PreminedLimits(ctx)
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{}, addr2limit)
	})

	t.Run("zero amount does not work", func(t *testing.T) {
		var premined [2]common.SpfUtxo
		f.Fuzz(&premined)
		premined[0].Value = types.ZeroCurrency
		require.Error(t, tdb.InsertPremined(ctx, premined[:]))
	})

	var premined [2]common.SpfUtxo
	f.Fuzz(&premined)

	t.Run("add two addresses with premined limits", func(t *testing.T) {
		require.NoError(t, tdb.InsertPremined(ctx, premined[:]))
	})

	t.Run("get premined limits - exist, but not used yet", func(t *testing.T) {
		addr2limit, err := tdb.PreminedLimits(ctx)
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit: premined[0].Value,
			},
			premined[1].UnlockHash: common.PreminedRecord{
				Limit: premined[1].Value,
			},
		}, addr2limit)
	})

	t.Run("get premined limit for one address", func(t *testing.T) {
		addr2limit, err := tdb.FindPremined(ctx, []types.UnlockHash{premined[0].UnlockHash})
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit: premined[0].Value,
			},
		}, addr2limit)
	})

	t.Run("UncompletedPremined (empty)", func(t *testing.T) {
		reqs, err := tdb.UncompletedPremined(ctx)
		require.NoError(t, err)
		require.Empty(t, reqs)
	})

	t.Run("add unconfirmed tx using 1/3 of limit of first address", func(t *testing.T) {
		info := &common.UnconfirmedTxInfo{}
		f.Fuzz(info)
		info.PreminedAddr = &premined[0].UnlockHash
		info.Type = common.Premined

		t.Run("zero amount does not work", func(t *testing.T) {
			info.Amount = types.ZeroCurrency
			_, err := tdb.AddUnconfirmedScpTx(ctx, info)
			require.Error(t, err)
		})

		info.Amount = premined[0].Value.Div64(3)
		queueAllowance, err := tdb.AddUnconfirmedScpTx(ctx, info)
		require.NoError(t, err)
		require.Nil(t, queueAllowance)
	})

	t.Run("get premined limit for the used address", func(t *testing.T) {
		t.Skip("TODO(zer0main): what should it return? I guess 2/3 of original amount?")
		addr2limit, err := tdb.FindPremined(ctx, []types.UnlockHash{premined[0].UnlockHash})
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit: premined[0].Value,
			},
		}, addr2limit)
	})

	t.Run("UncompletedPremined after adding unconfirmed tx (empty)", func(t *testing.T) {
		t.Skip("TODO(zer0main): should the queue be empty at this point?")
		reqs, err := tdb.UncompletedPremined(ctx)
		require.NoError(t, err)
		require.Empty(t, reqs)
	})
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
