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
		tdb.Close()
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
		for info.Amount.IsZero() {
			f.Fuzz(info)
		}
		info.PreminedAddr = nil
		info.Type = common.Regular
		_, err := tdb.AddUnconfirmedScpTx(ctx, info)
		require.NoError(t, err)
		infos = append(infos, info)
	}

	var wg sync.WaitGroup
	wg.Add(len(infos))

	// Round, because postgres stores with microsecond precision.
	now := time.Now().UTC().Round(time.Microsecond)

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
		for info.Amount.IsZero() {
			f.Fuzz(info)
		}
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

func TestIntegrationRegular(t *testing.T) {
	f := defaultFuzzer()
	tdb := NewTestTransporterDB(t, defaultSettings)
	ctx := context.Background()

	t.Run("empty queue size", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "0", queueSize.String())
	})

	t.Run("CheckAllowance in the beginning", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)
	})

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "0", queueSize.String())
	})

	info := &common.UnconfirmedTxInfo{}

	t.Run("add unconfirmed tx sending 100 funds", func(t *testing.T) {
		f.Fuzz(info)
		info.Type = common.Regular
		info.PreminedAddr = nil
		info.Height = nil

		t.Run("zero amount does not work", func(t *testing.T) {
			info.Amount = types.ZeroCurrency
			_, err := tdb.AddUnconfirmedScpTx(ctx, info)
			require.Error(t, err)
		})

		info.Amount = types.NewCurrency64(100)

		queueAllowance, err := tdb.AddUnconfirmedScpTx(ctx, info)
		require.NoError(t, err)
		require.Equal(t, &common.QueueAllowance{
			FreeCapacity: defaultSettings.QueueSizeLimit,
		}, queueAllowance)
	})

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "100", queueSize.String())
	})

	t.Run("NextInQueue", func(t *testing.T) {
		reqs, err := tdb.NextInQueue(ctx, types.ZeroCurrency)
		require.NoError(t, err)
		require.Empty(t, reqs)

		reqs, err = tdb.NextInQueue(ctx, info.Amount)
		require.NoError(t, err)
		require.Empty(t, reqs)

		reqs, err = tdb.NextInQueue(ctx, info.Amount.Add64(1))
		require.NoError(t, err)
		require.Empty(t, reqs)
	})

	t.Run("CheckAllowance after AddUnconfirmedScpTx", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit.Sub64(100),
				QueueSize:    types.NewCurrency64(100),
			},
		}, allowance)
	})

	t.Run("UnconfirmedInfo after AddUnconfirmedScpTx", func(t *testing.T) {
		info2, err := tdb.UnconfirmedInfo(ctx, info.BurnID)
		require.NoError(t, err)
		require.Equal(t, info, info2)
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

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "100", queueSize.String())
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

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "100", queueSize.String())
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
		// Round, because postgres stores with microsecond precision.
		now := time.Now().UTC().Round(time.Microsecond)

		reqs, err := tdb.ConfirmUnconfirmed(ctx, []common.UnconfirmedTxInfo{*info}, now)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{transportRequest}, reqs)

		transportRequest.QueueUpTime = &now
	})

	t.Run("NextInQueue", func(t *testing.T) {
		reqs, err := tdb.NextInQueue(ctx, types.ZeroCurrency)
		require.NoError(t, err)
		require.Empty(t, reqs)

		reqs, err = tdb.NextInQueue(ctx, info.Amount)
		require.NoError(t, err)
		require.Empty(t, reqs)

		reqs, err = tdb.NextInQueue(ctx, info.Amount.Add64(1))
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{transportRequest}, reqs)
	})

	t.Run("UnconfirmedBefore after ConfirmUnconfirmed", func(t *testing.T) {
		infos, err := tdb.UnconfirmedBefore(ctx, info.Time.Add(time.Second))
		require.NoError(t, err)
		require.Empty(t, infos)
	})

	t.Run("CheckAllowance after ConfirmUnconfirmed", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit.Sub64(100),
				QueueSize:    types.NewCurrency64(100),
			},
		}, allowance)
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

	t.Run("AddSolanaTransaction", func(t *testing.T) {
		for solanaTxInfo.SolanaTx == "" || solanaTxInfo.BroadcastTime == (time.Time{}) {
			f.Fuzz(&solanaTxInfo)
		}
		require.NoError(t, tdb.AddSolanaTransaction(ctx, info.BurnID, common.Regular, solanaTxInfo))
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

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "100", queueSize.String())
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

	t.Run("ConfirmedSupply", func(t *testing.T) {
		supplyInfo, err := tdb.ConfirmedSupply(ctx)
		require.NoError(t, err)
		require.Equal(t, common.SupplyInfo{
			Regular: info.Amount,
		}, supplyInfo)
	})

	t.Run("QueueSize", func(t *testing.T) {
		queueSize, err := tdb.QueueSize(ctx)
		require.NoError(t, err)
		require.Equal(t, "0", queueSize.String())
	})
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
	// Make sure the values are >= 3. It is needed because we
	// devide by 3 to get the number of funds to use and expect
	// some coins to be left.
	premined[0].Value = premined[0].Value.Add64(3)
	premined[1].Value = premined[1].Value.Add64(3)

	t.Run("CheckAllowance before adding limits", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)

		_, err = tdb.CheckAllowance(ctx, []types.UnlockHash{
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
		if info.Amount.IsZero() {
			info.Amount = types.NewCurrency64(1)
		}

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

	t.Run("get premined limits - something was used", func(t *testing.T) {
		addr2limit, err := tdb.PreminedLimits(ctx)
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[0].UnlockHash: common.PreminedRecord{
				Limit:       premined[0].Value,
				Transported: info.Amount,
			},
			premined[1].UnlockHash: common.PreminedRecord{
				Limit: premined[1].Value,
			},
		}, addr2limit)
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
		// Round, because postgres stores with microsecond precision.
		now := time.Now().UTC().Round(time.Microsecond)

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

	info2 := &common.UnconfirmedTxInfo{}

	t.Run("add unconfirmed tx using 2/3 of limit of first address", func(t *testing.T) {
		f.Fuzz(info2)
		info2.PreminedAddr = &premined[0].UnlockHash
		info2.Type = common.Premined
		info2.Height = nil
		info2.Amount = premined[0].Value.Sub(info.Amount)
		require.False(t, info2.Amount.IsZero())

		queueAllowance, err := tdb.AddUnconfirmedScpTx(ctx, info2)
		require.NoError(t, err)
		require.Nil(t, queueAllowance)
	})

	t.Run("get premined limits - first address is fully used", func(t *testing.T) {
		addr2limit, err := tdb.PreminedLimits(ctx)
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{
			premined[1].UnlockHash: common.PreminedRecord{
				Limit: premined[1].Value,
			},
		}, addr2limit)
	})

	t.Run("SetConfirmationHeight second tx", func(t *testing.T) {
		var confirmationHeight types.BlockHeight = 1001000
		require.NoError(t, tdb.SetConfirmationHeight(ctx, []types.TransactionID{info2.BurnID}, confirmationHeight))
		info2.Height = &confirmationHeight
	})

	t.Run("RemoveUnconfirmed second tx", func(t *testing.T) {
		require.NoError(t, tdb.RemoveUnconfirmed(ctx, []common.UnconfirmedTxInfo{*info2}))
	})

	t.Run("UnconfirmedInfo after RemoveUnconfirmed", func(t *testing.T) {
		_, err := tdb.UnconfirmedInfo(ctx, info2.BurnID)
		require.ErrorContains(t, err, "not exists")
	})

	t.Run("add second tx again", func(t *testing.T) {
		info2.Height = nil
		queueAllowance, err := tdb.AddUnconfirmedScpTx(ctx, info2)
		require.NoError(t, err)
		require.Nil(t, queueAllowance)
	})

	t.Run("SetConfirmationHeight second tx again", func(t *testing.T) {
		var confirmationHeight types.BlockHeight = 1001000
		require.NoError(t, tdb.SetConfirmationHeight(ctx, []types.TransactionID{info2.BurnID}, confirmationHeight))
		info2.Height = &confirmationHeight

		// Set it again to test such scenario.
		confirmationHeight++
		require.NoError(t, tdb.SetConfirmationHeight(ctx, []types.TransactionID{info2.BurnID}, confirmationHeight))
		info2.Height = &confirmationHeight
	})

	transportRequest2 := common.TransportRequest{
		SpfxInvoice: common.SpfxInvoice{
			Address:     info2.SolanaAddr,
			Amount:      info2.Amount,
			TotalSupply: info.Amount,
		},
		BurnID:   info2.BurnID,
		BurnTime: info2.Time,
		Type:     info2.Type,
	}

	t.Run("ConfirmUnconfirmed second tx", func(t *testing.T) {
		// Round, because postgres stores with microsecond precision.
		now := time.Now().UTC().Round(time.Microsecond)

		reqs, err := tdb.ConfirmUnconfirmed(ctx, []common.UnconfirmedTxInfo{*info2}, now)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{transportRequest2}, reqs)
	})

	t.Run("UnconfirmedBefore after ConfirmUnconfirmed second tx", func(t *testing.T) {
		infos, err := tdb.UnconfirmedBefore(ctx, info2.Time.Add(time.Second))
		require.NoError(t, err)
		require.Empty(t, infos)
	})

	t.Run("CheckAllowance after ConfirmUnconfirmed second tx", func(t *testing.T) {
		allowance, err := tdb.CheckAllowance(ctx, []types.UnlockHash{
			premined[0].UnlockHash,
			premined[1].UnlockHash,
		})
		require.NoError(t, err)
		require.Equal(t, &common.Allowance{
			AirdropFreeCapacity: airdropFreeCapacity,
			PreminedFreeCapacity: map[types.UnlockHash]types.Currency{
				premined[0].UnlockHash: premined[0].Value.Sub(premined[0].Value),
				premined[1].UnlockHash: premined[1].Value,
			},
			Queue: common.QueueAllowance{
				FreeCapacity: defaultSettings.PreliminaryQueueSizeLimit,
			},
		}, allowance)
	})

	t.Run("get premined limit for the used address after second tx", func(t *testing.T) {
		addr2limit, err := tdb.FindPremined(ctx, []types.UnlockHash{premined[0].UnlockHash})
		require.NoError(t, err)
		require.Equal(t, map[types.UnlockHash]common.PreminedRecord{}, addr2limit)
	})

	t.Run("UncompletedPremined after confirming second tx (non-empty)", func(t *testing.T) {
		reqs, err := tdb.UncompletedPremined(ctx)
		require.NoError(t, err)
		require.Equal(t, []common.TransportRequest{
			transportRequest,
			transportRequest2,
		}, reqs)
	})

	t.Run("TransportRecord after confirming second tx", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info2.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest2,
		}, transportRecord)
	})

	t.Run("UnconfirmedInfo after confirming second tx", func(t *testing.T) {
		_, err := tdb.UnconfirmedInfo(ctx, info2.BurnID)
		require.ErrorContains(t, err, "not exists")
	})

	t.Run("RecordsWithUnconfirmedSolana before solana broadcast", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	var solanaTxInfo common.SolanaTxInfo

	t.Run("AddSolanaTransaction", func(t *testing.T) {
		t.Run("empty solana txid is not accepted", func(t *testing.T) {
			err := tdb.AddSolanaTransaction(ctx, info.BurnID, common.Premined, solanaTxInfo)
			require.ErrorContains(t, err, "id_not_empty")
		})

		for solanaTxInfo.SolanaTx == "" || solanaTxInfo.BroadcastTime == (time.Time{}) {
			f.Fuzz(&solanaTxInfo)
		}

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

	var solanaTxInfo2 common.SolanaTxInfo

	t.Run("AddSolanaTransaction second tx", func(t *testing.T) {
		for solanaTxInfo2.SolanaTx == "" || solanaTxInfo2.BroadcastTime == (time.Time{}) {
			f.Fuzz(&solanaTxInfo2)
		}
		require.NoError(t, tdb.AddSolanaTransaction(ctx, info2.BurnID, common.Premined, solanaTxInfo2))
	})

	t.Run("TransportRecord before solana removed", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info2.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest2,
			SolanaTxInfo:     solanaTxInfo2,
		}, transportRecord)
	})

	t.Run("RemoveSolanaTransaction second tx", func(t *testing.T) {
		require.NoError(t, tdb.RemoveSolanaTransaction(ctx, info2.BurnID, common.Premined))
	})

	t.Run("TransportRecord after solana removed", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info2.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest2,
		}, transportRecord)
	})

	t.Run("RecordsWithUnconfirmedSolana after solana removed", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	var solanaTxInfo2a common.SolanaTxInfo

	t.Run("AddSolanaTransaction second tx", func(t *testing.T) {
		for solanaTxInfo2a.SolanaTx == "" || solanaTxInfo2a.BroadcastTime == (time.Time{}) {
			f.Fuzz(&solanaTxInfo2a)
		}
		require.NoError(t, tdb.AddSolanaTransaction(ctx, info2.BurnID, common.Premined, solanaTxInfo2a))
	})

	solanaConfirm2 := solanaTxInfo2a.BroadcastTime.Add(time.Minute)

	t.Run("ConfirmSolana second tx", func(t *testing.T) {
		require.NoError(t, tdb.ConfirmSolana(ctx, solanaTxInfo2a.SolanaTx, solanaConfirm2))
	})

	t.Run("TransportRecord after solana confirmation of second tx", func(t *testing.T) {
		transportRecord, err := tdb.TransportRecord(ctx, info2.BurnID)
		require.NoError(t, err)
		require.Equal(t, &common.TransportRecord{
			TransportRequest: transportRequest2,
			SolanaTxInfo:     solanaTxInfo2a,
			ConfirmationTime: solanaConfirm2,
			Completed:        true,
		}, transportRecord)
	})

	t.Run("RecordsWithUnconfirmedSolana after solana confirmation of second tx", func(t *testing.T) {
		records, err := tdb.RecordsWithUnconfirmedSolana(ctx)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("ConfirmedSupply", func(t *testing.T) {
		supplyInfo, err := tdb.ConfirmedSupply(ctx)
		require.NoError(t, err)
		require.Equal(t, common.SupplyInfo{
			Premined: premined[0].Value,
		}, supplyInfo)
	})
}
