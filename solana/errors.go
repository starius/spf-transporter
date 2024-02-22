package solana

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

var (
	ErrTimeout = fmt.Errorf("timeout")

	// ErrWalletNotFound indicates that wallet address is not found on the blockchain.
	ErrWalletNotFound = fmt.Errorf("wallet not found")

	// ErrNotWalletAddress indicates that provided address is not owned by
	// System Program, hence is not a Wallet Address.
	ErrNotWalletAddress = fmt.Errorf("not a wallet address")

	// ErrATANotFound indicates that associated token account not found
	// or has 0 balance.
	ErrATANotFound = fmt.Errorf("associated token account not funded")

	// ErrInvalidSupply indicates that incorrect supply number was
	// provided during mint. Consider checking result of SupplyInfo().
	ErrInvalidSupply = fmt.Errorf("invalid supply")

	// ErrTotalSupplyOverflow indicates that mint would result in total supply
	// overflow.
	ErrTotalSupplyOverflow = fmt.Errorf("total supply overflow")

	// ErrInvalidAirdropSupply indicates that given supply of tokens
	// is incorrect when initializing airdrop.
	ErrInvalidAirdropSupply = fmt.Errorf("invalid airdrop supply")

	// ErrAirdropAlreadyInitialized indicates that airdrop has been already
	// initialized.
	ErrAirdropAlreadyInitialized = fmt.Errorf("airdrop already initialized")

	// ErrTimeRangeViolation indicates that specified mint has not started or
	// already ended.
	ErrTimeRangeViolation = fmt.Errorf("time range violation")

	// ErrSupplyOverflow indicates that specified mint would result supply
	// overflow.
	ErrSupplyOverflow = fmt.Errorf("supply overflow")

	// ErrAmountTooSmall indicates that given amount is too small.
	ErrAmountTooSmall = fmt.Errorf("amount too small")

	// ErrAmountTooBig indicates that given amount is too big.
	ErrAmountTooBig = fmt.Errorf("amount too big")
)

// https://gitlab.com/scpcorp/solana-token/-/blob/main/contracts/minter/src/error.rs
var customErrorMap = map[int]error{
	2000: ErrInvalidSupply,
	2001: ErrTotalSupplyOverflow,
	2002: ErrATANotFound,
	2003: ErrInvalidAirdropSupply,
	2004: ErrAirdropAlreadyInitialized,
	2005: ErrTimeRangeViolation,
	2006: ErrSupplyOverflow,
	2007: ErrAmountTooSmall,
	2008: ErrAmountTooBig,
}

func parsePreflightError(origErr error) error {
	var rpcErr *jsonrpc.RPCError
	if !errors.As(origErr, &rpcErr) {
		return origErr
	}
	dataMap, ok := rpcErr.Data.(map[string]interface{})
	if !ok {
		return origErr
	}
	errVal, ok := dataMap["err"]
	if !ok {
		return origErr
	}
	if err := parseErrorValue(errVal); err != nil {
		return err
	}
	return origErr
}

func parseErrorValue(errorValue interface{}) error {
	if errorValue == nil {
		return nil
	}
	errMap, ok := errorValue.(map[string]interface{})
	if !ok {
		return nil
	}
	instructionErrorVal, ok := errMap["InstructionError"]
	if !ok {
		return nil
	}
	instructionErrorSlice, ok := instructionErrorVal.([]interface{})
	if !ok {
		return nil
	}
	if len(instructionErrorSlice) < 2 {
		return nil
	}
	if err := decodeCustomError(instructionErrorSlice); err != nil {
		return err
	}
	return nil
}

func decodeCustomError(instructionErrorSlice []interface{}) error {
	customErrorStructMap, ok := instructionErrorSlice[1].(map[string]interface{})
	if !ok {
		return nil
	}
	if len(customErrorStructMap) != 1 {
		return nil
	}
	errorCodeRaw, ok := customErrorStructMap["Custom"]
	if !ok {
		return nil
	}

	var errorCode int
	switch errorCodeNum := errorCodeRaw.(type) {
	case json.Number: // This type comes from a Preflight error
		errorCode64, err2 := errorCodeNum.Int64()
		if err2 != nil {
			return nil
		}
		errorCode = int(errorCode64)
	case float64: // This type comes from a Transaction error
		errorCode = int(errorCodeNum)
	default:
		return nil
	}

	mappedErr, ok := customErrorMap[errorCode]
	if !ok {
		return nil
	}
	return mappedErr
}
