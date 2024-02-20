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

var airdropFreeCapacity = types.NewCurrency64(15000000)

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

func TestIntegrationFlags(t *testing.T) {
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	flag, err := tdb.GetFlag(ctx, "test_flag")
	require.NoError(t, err)
	require.False(t, flag)

	require.NoError(t, tdb.SetFlag(ctx, "test_flag", true))

	flag, err = tdb.GetFlag(ctx, "test_flag")
	require.NoError(t, err)
	require.True(t, flag)
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
		var premined [2]common.SpfAddressBalance
		f.Fuzz(&premined)
		premined[0].Value = types.ZeroCurrency
		require.Error(t, tdb.InsertPremined(ctx, premined[:]))
	})

	var premined [2]common.SpfAddressBalance
	f.Fuzz(&premined)

	t.Run("CheckAllowance before adding limits", func(t *testing.T) {
		_, err := tdb.CheckAllowance(ctx, []types.UnlockHash{
			premined[0].UnlockHash,
			premined[1].UnlockHash,
		})
		require.ErrorContains(t, err, "not all of provided unlock hashes are premined")
	})

	t.Run("add two addresses with premined limits", func(t *testing.T) {
		require.NoError(t, tdb.InsertPremined(ctx, premined[:]))
	})

	t.Run("CheckAllowance after adding limits", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{
			premined[0].UnlockHash,
			premined[1].UnlockHash,
		})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			PreminedFreeCapacity: map[types.UnlockHash]types.Currency{
				premined[0].UnlockHash: premined[0].Value,
				premined[1].UnlockHash: premined[1].Value,
			},
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)
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

	info := &common.UnconfirmedTxInfo{}

	t.Run("add unconfirmed tx using 1/3 of limit of first address", func(t *testing.T) {
		f.Fuzz(info)
		info.PreminedAddr = &premined[0].UnlockHash
		info.Type = common.Premined
		info.Height = nil

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

	t.Run("UnconfirmedInfo after AddUnconfirmedScpTx", func(t *testing.T) {
		info2, err := tdb.UnconfirmedInfo(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, info, info2)
	})

	t.Run("CheckAllowance after AddUnconfirmedScpTx", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{
			premined[0].UnlockHash,
			premined[1].UnlockHash,
		})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			PreminedFreeCapacity: map[types.UnlockHash]types.Currency{
				premined[0].UnlockHash: premined[0].Value.Sub(info.Amount),
				premined[1].UnlockHash: premined[1].Value,
			},
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)
	})

	t.Run("get premined limit for the used address", func(t *testing.T) {
		addr2limit, err := tdb.FindPremined(ctx, []types.UnlockHash{premined[0].UnlockHash})
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit:       premined[0].Value,
				Transported: info.Amount,
			},
		}, addr2limit)
	})

	t.Run("UncompletedPremined after adding unconfirmed tx (empty)", func(t *testing.T) {
		reqs, err := tdb.UncompletedPremined(ctx)
		require.NoError(t, err)
		require.Empty(t, reqs)
	})

	t.Run("TransportRecord before confirming tx", func(t *testing.T) {
		_, err := tdb.TransportRecord(ctx, info.BurnID)
		require.ErrorContains(t, err, "not exists")
	})

	t.Run("UnconfirmedBefore before SetConfirmationHeight", func(t *testing.T) {
		infos, err := tdb.UnconfirmedBefore(ctx, info.Time)
		require.NoError(t, err)
		require.Empty(t, infos)

		infos, err = tdb.UnconfirmedBefore(ctx, info.Time.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, []common.UnconfirmedTxInfo{*info}, infos)
	})

	t.Run("SetConfirmationHeight", func(t *testing.T) {
		var confirmationHeight types.BlockHeight = 1000000
		require.NoError(t, tdb.SetConfirmationHeight(ctx, []types.TransactionID{info.BurnID}, confirmationHeight))
		info.Height = &confirmationHeight
	})

	t.Run("UnconfirmedInfo after SetConfirmationHeight", func(t *testing.T) {
		info2, err := tdb.UnconfirmedInfo(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, info, info2)
	})

	t.Run("UnconfirmedBefore before ConfirmUnconfirmed", func(t *testing.T) {
		infos, err := tdb.UnconfirmedBefore(ctx, info.Time.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, []common.UnconfirmedTxInfo{*info}, infos)
	})

	transportRequest := common.TransportRequest{
		SpfxInvoice: common.SpfxInvoice{
			Address: info.SolanaAddr,
			Amount:  info.Amount,
			// TotalSupply is 0.
		},
		BurnID:   info.BurnID,
		BurnTime: info.Time,
		Type:     info.Type,
	}

	t.Run("ConfirmUnconfirmed", func(t *testing.T) {
		now := time.Now()
		reqs, err := tdb.ConfirmUnconfirmed(ctx, []common.UnconfirmedTxInfo{*info}, now)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{transportRequest}, reqs)
	})

	t.Run("UnconfirmedBefore after ConfirmUnconfirmed", func(t *testing.T) {
		infos, err := tdb.UnconfirmedBefore(ctx, info.Time.Add(time.Second))
		require.NoError(t, err)
		require.Empty(t, infos)
	})

	t.Run("CheckAllowance after ConfirmUnconfirmed", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{
			premined[0].UnlockHash,
			premined[1].UnlockHash,
		})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			PreminedFreeCapacity: map[types.UnlockHash]types.Currency{
				premined[0].UnlockHash: premined[0].Value.Sub(info.Amount),
				premined[1].UnlockHash: premined[1].Value,
			},
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)
	})

	t.Run("get premined limit for the used address again", func(t *testing.T) {
		addr2limit, err := tdb.FindPremined(ctx, []types.UnlockHash{premined[0].UnlockHash})
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit:       premined[0].Value,
				Transported: info.Amount,
			},
		}, addr2limit)
	})

	t.Run("UncompletedPremined after confirming tx (non-empty)", func(t *testing.T) {
		reqs, err := tdb.UncompletedPremined(ctx)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{transportRequest}, reqs)
	})

	t.Run("TransportRecord after confirming tx", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest,
		}, transportRecord)
	})

	t.Run("UnconfirmedInfo after confirming tx", func(t *testing.T) {
		_, err := tdb.UnconfirmedInfo(ctx, info.BurnID)
		require.ErrorContains(t, err, "not exists")
	})

	t.Run("RecordsWithUnconfirmedSolana before solana broadcast", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	var solanaTxInfo common.SolanaTxInfo
	f.Fuzz(&solanaTxInfo)

	t.Run("AddSolanaTransaction", func(t *testing.T) {
		require.NoError(t, tdb.AddSolanaTransaction(ctx, info.BurnID, common.Premined, solanaTxInfo))
	})

	t.Run("TransportRecord after solana broadcast", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest,
			SolanaTxInfo:     solanaTxInfo,
		}, transportRecord)
	})

	t.Run("RecordsWithUnconfirmedSolana after solana broadcast", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRecord{{
			TransportRequest: transportRequest,
			SolanaTxInfo:     solanaTxInfo,
		}}, records)
	})

	solanaConfirm := solanaTxInfo.BroadcastTime.Add(time.Minute)

	t.Run("ConfirmSolana", func(t *testing.T) {
		require.NoError(t, tdb.ConfirmSolana(ctx, solanaTxInfo.SolanaTx, solanaConfirm))
	})

	t.Run("TransportRecord after solana confirmation", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest,
			SolanaTxInfo:     solanaTxInfo,
			ConfirmationTime: solanaConfirm,
			Completed:        true,
		}, transportRecord)
	})

	t.Run("RecordsWithUnconfirmedSolana after solana confirmation", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Empty(t, records)
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

	var wantPremined []common.SpfAddressBalance
	f.Fuzz(&wantPremined)
	t.Logf("Inserting %d premined UTXOs)", len(wantPremined))
	require.NoError(t, tdb.InsertPremined(ctx, wantPremined))
	premined, err = tdb.PreminedWhitelist(ctx)
	require.NoError(t, err)
	require.Equal(t, wantPremined, premined)

	var newPremined []common.SpfAddressBalance
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
