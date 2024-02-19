package common

import (
	"errors"
	"fmt"
	"time"

	"github.com/mr-tron/base58"
	"gitlab.com/scpcorp/ScPrime/types"
)

var ErrNotExists = errors.New("not exists")

type QueueHandlingMode int

const (
	OneByOne         QueueHandlingMode = iota // Queue is released continuously as old records leave.
	ReleaseAllAtOnce                          // Queue is locked after it reaches QueueLockLimit from settings.
)

type TransportType int

const (
	Airdrop TransportType = iota
	Premined
	Regular

	TransportTypeCount
)

type TransportStatus int

const (
	NotFound TransportStatus = iota
	Unconfirmed
	InTheQueue
	SolanaTxCreated
	Completed

	TransportStatusCount
)

type PublicKey string

type SupplyInfo struct {
	Airdrop  types.Currency `json:"airdrop_supply"`
	Premined types.Currency `json:"premined_supply"`
	Regular  types.Currency `json:"regular_supply"`
}

func (si SupplyInfo) Equals(si2 SupplyInfo) bool {
	if si.Airdrop.Cmp(si2.Airdrop) != 0 {
		return false
	}
	if si.Premined.Cmp(si2.Premined) != 0 {
		return false
	}
	if si.Regular.Cmp(si2.Regular) != 0 {
		return false
	}
	return true
}

type SpfUtxo struct {
	Value      types.Currency   `json:"value"`
	UnlockHash types.UnlockHash `json:"unlockhash"`
}

type PreminedRecord struct {
	Limit       types.Currency `json:"limit"`
	Transported types.Currency `json:"transported"`
}

const SolanaAddrLen = 32

type SolanaAddress [SolanaAddrLen]byte

func SolanaAddressFromString(addrStr string) (addr SolanaAddress, err error) {
	val, err := base58.Decode(addrStr)
	if err != nil {
		return addr, fmt.Errorf("decode: %w", err)
	}
	if len(val) != SolanaAddrLen {
		return addr, fmt.Errorf("invalid length, expected %v, got %d", SolanaAddrLen, len(val))
	}
	copy(addr[:], val)
	return
}

func (addr SolanaAddress) String() string {
	return base58.Encode(addr[:])
}

type SpfxInvoice struct {
	Address     SolanaAddress  `json:"address"`
	Amount      types.Currency `json:"amount"`
	TotalSupply types.Currency `json:"total_supply"`
}

func (inv *SpfxInvoice) SupplyAfter() types.Currency {
	return inv.TotalSupply.Add(inv.Amount)
}

type TransportRequest struct {
	SpfxInvoice
	BurnID      types.TransactionID `json:"burn_id"`
	BurnTime    time.Time           `json:"burn_time"`
	QueueUpTime *time.Time          `json:"queue_up_time,omitempty"`
	Type        TransportType       `json:"type"`
}

type SolanaTxID string

type SolanaTxStatus struct {
	ConfirmationTime time.Time `json:"confirmation_time"`
	Confirmed        bool      `json:"confirmed"`
	MintSuccessful   bool      `json:"mint_successful"`
}

type SolanaTxInfo struct {
	BroadcastTime time.Time  `json:"broadcast_time"`
	SolanaTx      SolanaTxID `json:"solana_tx_id"`
}

type TransportRecord struct {
	TransportRequest
	SolanaTxInfo
	ConfirmationTime time.Time `json:"confirmation_time"`
	Completed        bool      `json:"completed"`
}

type UnconfirmedTxInfo struct {
	BurnID       types.TransactionID `json:"burn_id"`
	Amount       types.Currency      `json:"amount"`
	SolanaAddr   SolanaAddress       `json:"solana_addr"`
	Height       *types.BlockHeight  `json:"block_height"`
	Time         time.Time           `json:"time"`
	PreminedAddr *types.UnlockHash   `json:"premined_addr"`
	Type         TransportType       `json:"type"`
}

type QueueAllowance struct {
	FreeCapacity types.Currency `json:"free_capacity"`
	QueueSize    types.Currency `json:"queue_size"`
}

type Allowance struct {
	AirdropFreeCapacity  types.Currency                      `json:"airdrop_free_capacity"`
	PreminedFreeCapacity map[types.UnlockHash]types.Currency `json:"premined_free_capacity"`
	Queue                QueueAllowance                      `json:"queue"`
}
