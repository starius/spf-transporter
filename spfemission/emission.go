package spfemission

import (
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

// TODO: confirm all the constants before release!
var (
	TransporterStart = time.Date(2024, time.January, 29, 0, 0, 0, 0, time.UTC)
	EmissionStart    = time.Date(2024, time.February, 22, 0, 0, 0, 0, time.UTC)
	StartSupply      = types.NewCurrency64(45000000)
	SpfPerMinute     = types.NewCurrency64(60)
	MaxSupply        = types.NewCurrency64(200000000)
)

type EmissionRules struct {
	timeNow func() time.Time
}

func New() *EmissionRules {
	return &EmissionRules{timeNow: time.Now}
}

func (r *EmissionRules) AllowedSupply() types.Currency {
	now := r.timeNow().UTC()
	if now.Before(EmissionStart) {
		return StartSupply
	}
	minutesPassed := int64(now.Sub(EmissionStart) / time.Minute) // Always round down.
	emission := StartSupply.Add(SpfPerMinute.Mul64(uint64(minutesPassed)))
	if emission.Cmp(MaxSupply) > 0 {
		return MaxSupply
	}
	return emission
}

func divCurrencyRoundUp(c, divBy types.Currency) types.Currency {
	return c.Add(divBy.Sub64(1)).Div(divBy)
}

func (r *EmissionRules) SupplyTime(supply types.Currency) (time.Time, bool) {
	if supply.Cmp(MaxSupply) > 0 {
		return time.Time{}, false
	}
	if supply.Cmp(StartSupply) <= 0 {
		return TransporterStart, true
	}
	minutes, err := divCurrencyRoundUp(supply.Sub(StartSupply), SpfPerMinute).Uint64()
	if err != nil {
		return time.Time{}, false
	}
	return EmissionStart.Add(time.Minute * time.Duration(int64(minutes))), true
}

func (r *EmissionRules) TimeTillSupply(supply types.Currency) (time.Duration, bool) {
	currentSupply := r.AllowedSupply()
	if currentSupply.Cmp(supply) >= 0 {
		return time.Duration(0), true
	}
	now := r.timeNow().UTC()
	supplyTime, ok := r.SupplyTime(supply)
	if !ok {
		return time.Duration(0), false
	}
	return supplyTime.Sub(now), true
}
