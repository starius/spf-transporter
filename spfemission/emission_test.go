package spfemission

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gitlab.com/scpcorp/ScPrime/types"
)

func TestAllowedSupply(t *testing.T) {
	er := &EmissionRules{}
	cases := []struct {
		now    time.Time
		supply types.Currency
	}{
		{now: TransporterStart, supply: StartSupply},
		{now: time.Date(2030, time.January, 12, 0, 0, 0, 0, time.UTC), supply: MaxSupply},
		{now: EmissionStart, supply: StartSupply},
		{now: EmissionStart.Add(time.Hour * 24 * 30), supply: types.NewCurrency64(47592000)},
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
		{supply: types.ZeroCurrency, time: TransporterStart, ok: true},
		{supply: MaxSupply, time: EmissionStart.Add(time.Minute * 2583334), ok: true},
		{supply: MaxSupply.Add(types.NewCurrency64(1)), time: time.Time{}, ok: false},
		{supply: MaxSupply.Mul(types.NewCurrency64(2)), time: time.Time{}, ok: false},
		{supply: StartSupply, time: TransporterStart, ok: true},
		{supply: StartSupply.Sub(types.NewCurrency64(1)), time: TransporterStart, ok: true},
		{supply: StartSupply.Add(types.NewCurrency64(1)), time: EmissionStart.Add(time.Minute), ok: true},
		{supply: StartSupply.Add(types.NewCurrency64(1200)), time: EmissionStart.Add(20 * time.Minute), ok: true},
	}
	for _, tc := range cases {
		time, ok := er.SupplyTime(tc.supply)
		require.Equal(t, tc.time, time)
		require.Equal(t, tc.ok, ok)
	}
}
