package spfemission

import (
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter/common"
)

type EmissionRules struct {
	timeNow func() time.Time
}

func New() *EmissionRules {
	return &EmissionRules{timeNow: time.Now}
}

func (r *EmissionRules) AllowedSupply() types.Currency {
	now := r.timeNow().UTC()
	if now.Before(common.EmissionStart) {
		return types.ZeroCurrency
	}
	minutesPassed := int64(now.Sub(common.EmissionStart) / time.Minute) // Always round down.
	emission := common.SpfPerMinute.Mul64(uint64(minutesPassed))
	if emission.Cmp(common.TotalSupply) > 0 {
		return common.TotalSupply
	}
	return emission
}

func (r *EmissionRules) SupplyTime(supply types.Currency) (time.Time, bool) {
	if supply.Cmp(common.TotalSupply) > 0 {
		return time.Time{}, false
	}
	minutes := common.DivCurrencyRoundUp(supply, common.SpfPerMinute).Big().Int64()
	return common.EmissionStart.Add(time.Minute * time.Duration(minutes)), true
}

func (r *EmissionRules) EmissionTime(supplyChange types.Currency) time.Duration {
	minutes := common.DivCurrencyRoundUp(supplyChange, common.SpfPerMinute).Big().Int64()
	return time.Minute * time.Duration(minutes)
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
