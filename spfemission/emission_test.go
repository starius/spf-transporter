package spfemission

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

func TestAllowedSupply(t *testing.T) {
	er := &EmissionRules{}
	cases := []struct {
		now    time.Time
		supply types.Currency
	}{
		{now: time.Date(2050, time.January, 12, 0, 0, 0, 0, time.UTC), supply: common.TotalSupply},
		{now: common.EmissionStart, supply: types.ZeroCurrency},
		{now: common.EmissionStart.Add(time.Hour * 24 * 30), supply: types.NewCurrency64(2592000)},
	}
	for _, tc := range cases {
		er.timeNow = func() time.Time {
			return tc.now
		}
		allowed := er.AllowedSupply()
		if !allowed.Equals(tc.supply) {
			t.Errorf("invalid supply %s; must be: %s", allowed.String(), tc.supply.String())
		}
	}
}

func TestSupplyTime(t *testing.T) {
	er := &EmissionRules{}
	cases := []struct {
		supply types.Currency
		time   time.Time
		ok     bool
	}{
		{supply: types.NewCurrency64(155000000), time: common.EmissionStart.Add(time.Minute * 2583334), ok: true},
		{supply: common.TotalSupply.Add(types.NewCurrency64(1)), time: time.Time{}, ok: false},
		{supply: common.TotalSupply.Mul(types.NewCurrency64(2)), time: time.Time{}, ok: false},
		{supply: types.NewCurrency64(2), time: common.EmissionStart.Add(time.Minute), ok: true},
		{supply: types.NewCurrency64(1), time: common.EmissionStart.Add(time.Minute), ok: true},
		{supply: types.NewCurrency64(1200), time: common.EmissionStart.Add(20 * time.Minute), ok: true},
	}
	for _, tc := range cases {
		time, ok := er.SupplyTime(tc.supply)
		require.Equal(t, tc.time, time)
		require.Equal(t, tc.ok, ok)
	}
}
