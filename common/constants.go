package common

import (
	"time"

	"gitlab.com/scpcorp/ScPrime/types"
)

// TODO: confirm all the constants before release!
var (
	EmissionStart = time.Date(2024, time.March, 21, 0, 0, 0, 0, time.UTC)
	SpfPerMinute  = types.NewCurrency64(60)
	TotalSupply   = types.NewCurrency64(400000000)
	PreminedMax   = types.NewCurrency64(30000000)
	AirdropMax    = types.NewCurrency64(15000000)
	// AirdropUnlockHash = types.UnlockHashFromAddrStr("")
)
