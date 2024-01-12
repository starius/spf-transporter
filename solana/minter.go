package solana

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

type minter struct {
	minterConfig
	decimalization uint64
	key            solana.PrivateKey
	rpc            *rpc.Client
	ws             *ws.Client
}

func NewMinter(ctx context.Context, config minterConfig) (*minter, error) {
	key, err := solana.PrivateKeyFromSolanaKeygenFile(config.SolanaKeygenFile)
	if err != nil {
		return nil, fmt.Errorf("cannot create private key: %w", err)
	}

	wsClient, err := ws.Connect(ctx, config.Cluster.WS)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to websocket: %w", err)
	}

	return &minter{
		minterConfig:   config,
		decimalization: uint64(intPow(10, config.Decimals)),
		key:            key,
		rpc:            rpc.New(config.Cluster.RPC),
		ws:             wsClient,
	}, nil
}

func (m *minter) InitializeAirdropTx(ctx context.Context, maxSupply int) (solana.Signature, *solana.Transaction, error) {
	mintingStateAddr, err := m.mintingStateAddr()
	if err != nil {
		return solana.Signature{}, nil, err
	}

	instructionData := instructionInitializeAirdrop{
		rawMaxSupply: uint64(maxSupply) * m.decimalization,
	}.InstructionData()

	instruction := solana.NewInstruction(
		m.MinterProgram,
		[]*solana.AccountMeta{
			{
				PublicKey:  m.key.PublicKey(),
				IsSigner:   true,
				IsWritable: true,
			},
			{
				PublicKey:  mintingStateAddr,
				IsSigner:   false,
				IsWritable: true,
			},
			{
				PublicKey:  solana.SystemProgramID,
				IsSigner:   false,
				IsWritable: false,
			},
		},
		instructionData,
	)

	return m.signTx(ctx, instruction)
}

// CheckWallet checks that provided wallet address is legit.
// Technically wallet account is not required to exist in order to receive
// tokens to an Associated Token Account, hence skipWalletAccountCheck flag.
// Note, that MintInitialTrancheTx and MintEmissionTx methods call this method
// with flag set to true.
func (m *minter) CheckWallet(ctx context.Context, walletAddr solana.PublicKey, amount uint64, skipWalletAccountCheck bool) error {
	if !skipWalletAccountCheck {
		walletAccount, err := m.rpc.GetAccountInfo(ctx, walletAddr)
		if err != nil {
			if errors.Is(err, rpc.ErrNotFound) {
				return ErrWalletNotFound
			}
			return fmt.Errorf("cannot get wallet account: %w", err)
		}

		if walletAccount.Value.Owner != solana.SystemProgramID {
			return ErrNotWalletAddress
		}
	}

	// If amount is big enough, there is no need to check for associated
	// token account, because if it's not there, it will be funded
	// by minter program.
	if amount >= m.FundATAMinAmount {
		return nil
	}

	ataAddr, err := m.findATA(walletAddr)
	if err != nil {
		return err
	}
	ata, err := m.rpc.GetAccountInfo(ctx, ataAddr)
	if err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return ErrATANotFound
		}
		return fmt.Errorf("cannot get ata account: %w", err)
	}

	if ata.Value.Lamports == 0 {
		// Account with no Rent is the same as if it didn't exist.
		return ErrATANotFound
	}

	return nil
}

func (m *minter) MintInitialTrancheTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error) {
	return m.mintTx(ctx, instructionNumMintInitialTranche, walletAddr, amount, minterSupply)
}

func (m *minter) MintEmissionTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error) {
	return m.mintTx(ctx, instructionNumMintEmission, walletAddr, amount, minterSupply)
}

func (m *minter) MintAirdropTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error) {
	return m.mintTx(ctx, instructionNumMintAirdrop, walletAddr, amount, minterSupply)
}

// mintTx builds transaction that mints given amount of tokens for given
// walletAddr. To prevent double-mint contract checks that current supply
// is equal to totalSupply. This method applies decimalization, e.g. if amount
// is 10, then 10_000 units (raw tokens) will be minted, given that token
// uses 3 decimals, resulting in "10" tokens in the user's wallet.
func (m *minter) mintTx(ctx context.Context, instructionNum byte, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error) {
	var instructionData []byte
	switch instructionNum {
	case instructionNumMintInitialTranche:
		instructionData = instructionMintInitialTranche{
			rawAmount:               amount * m.decimalization,
			rawInitialTrancheSupply: minterSupply * m.decimalization,
		}.InstructionData()
	case instructionNumMintEmission:
		instructionData = instructionMintEmission{
			rawAmount:         amount * m.decimalization,
			rawEmissionSupply: minterSupply * m.decimalization,
		}.InstructionData()
	case instructionNumMintAirdrop:
		instructionData = instructionMintAirdrop{
			rawAmount:        amount * m.decimalization,
			rawAirdropSupply: minterSupply * m.decimalization,
		}.InstructionData()
	default:
		panic("bad instructionNum")
	}

	err := m.CheckWallet(ctx, walletAddr, amount, true)
	if err != nil {
		return solana.Signature{}, nil, err
	}

	ataAddr, err := m.findATA(walletAddr)
	if err != nil {
		return solana.Signature{}, nil, err
	}

	mintAuthorityAddr, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(m.MintAuthoritySeed)},
		m.MinterProgram,
	)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("cannot derive mint authority: %w", err)
	}

	mintingStateAddr, err := m.mintingStateAddr()
	if err != nil {
		return solana.Signature{}, nil, err
	}

	instruction := solana.NewInstruction(
		m.MinterProgram,
		[]*solana.AccountMeta{
			{
				PublicKey:  m.key.PublicKey(),
				IsSigner:   true,
				IsWritable: false,
			},
			{
				PublicKey:  mintAuthorityAddr,
				IsSigner:   false,
				IsWritable: false,
			},
			{
				PublicKey:  m.Token,
				IsSigner:   false,
				IsWritable: true,
			},
			{
				PublicKey:  walletAddr,
				IsSigner:   false,
				IsWritable: false,
			},
			{
				PublicKey:  ataAddr,
				IsSigner:   false,
				IsWritable: true,
			},
			{
				PublicKey:  solana.TokenProgramID,
				IsSigner:   false,
				IsWritable: false,
			},
			{
				PublicKey:  mintingStateAddr,
				IsSigner:   false,
				IsWritable: true,
			},
			{
				PublicKey:  solana.SystemProgramID,
				IsSigner:   false,
				IsWritable: false,
			},
			{
				PublicKey:  solana.SPLAssociatedTokenAccountProgramID,
				IsSigner:   false,
				IsWritable: false,
			},
		},
		instructionData,
	)

	return m.signTx(ctx, instruction)
}

func (m *minter) SendAndConfirm(ctx context.Context, tx *solana.Transaction) error {
	opts := rpc.TransactionOpts{
		SkipPreflight:       false,
		PreflightCommitment: rpc.CommitmentFinalized,
	}
	sig, err := m.rpc.SendTransactionWithOpts(ctx, tx, opts)
	if err != nil {
		err = parsePreflightError(err)
		return fmt.Errorf("cannot send: %w", err)
	}

	err = waitForConfirmation(ctx, m.ws, sig)
	if err != nil {
		err = parsePreflightError(err)
		return fmt.Errorf("cannot wait: %w", err)
	}

	return nil
}

func waitForConfirmation(
	ctx context.Context,
	wsClient *ws.Client,
	sig solana.Signature,
) error {
	sub, err := wsClient.SignatureSubscribe(
		sig,
		rpc.CommitmentFinalized,
	)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	t := 2 * time.Minute // random default timeout
	timeout := &t

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(*timeout):
			return ErrTimeout
		case resp, ok := <-sub.Response():
			if !ok {
				return fmt.Errorf("subscription closed")
			}
			if resp.Value.Err != nil {
				if err := parseErrorValue(resp.Value.Err); err != nil {
					return err
				}
				// The transaction was confirmed, but it failed while executing (one of the instructions failed).
				return fmt.Errorf("confirmed transaction with execution error: %v", resp.Value.Err)
			} else {
				// Success! Confirmed! And there was no error while executing the transaction.
				return nil
			}
		case err := <-sub.Err():
			return err
		}
	}
}

func (m *minter) findATA(walletAddr solana.PublicKey) (solana.PublicKey, error) {
	ataAddr, _, err := solana.FindAssociatedTokenAddress(walletAddr, m.Token)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("cannot find ata: %w", err)
	}
	return ataAddr, nil
}

func (m *minter) mintingStateAddr() (solana.PublicKey, error) {
	mintingStateAddr, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(m.MintingStateSeed)},
		m.MinterProgram,
	)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("cannot derive minting state account: %w", err)
	}
	return mintingStateAddr, nil
}

type SupplyInfo struct {
	TotalSupply          uint64
	InitialTrancheSupply uint64
	AirdropSupply        uint64
	EmissionSupply       uint64
	AirdropMaxSupply     uint64
}

type mintingState struct {
	RawInitialTrancheSupply uint64
	RawAirdropSupply        uint64
	RawEmissionSupply       uint64
	RawAirdropMaxSupply     uint64
}

// SupplyInfo returns supply numbers.
func (m *minter) SupplyInfo(ctx context.Context) (SupplyInfo, error) {
	var mint token.Mint
	err := m.rpc.GetAccountDataInto(ctx, m.Token, &mint)
	if err != nil {
		return SupplyInfo{}, fmt.Errorf("cannot get token mint account: %w", err)
	}

	mintingStateAddr, err := m.mintingStateAddr()
	if err != nil {
		return SupplyInfo{}, err
	}
	var em mintingState
	err = m.rpc.GetAccountDataInto(ctx, mintingStateAddr, &em)
	if err != nil && err != rpc.ErrNotFound {
		return SupplyInfo{}, fmt.Errorf("cannot get minting state account: %w", err)
	}

	return SupplyInfo{
		TotalSupply:          mint.Supply / m.decimalization,
		InitialTrancheSupply: em.RawInitialTrancheSupply / m.decimalization,
		AirdropSupply:        em.RawAirdropSupply / m.decimalization,
		EmissionSupply:       em.RawEmissionSupply / m.decimalization,
		AirdropMaxSupply:     em.RawAirdropMaxSupply / m.decimalization,
	}, nil
}

type Status struct {
	Confirmed        bool
	MintSuccessful   bool
	ConfirmationTime time.Time
}

func (m *minter) TxStatus(ctx context.Context, sig solana.Signature) (Status, error) {
	res, err := m.rpc.GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
		Commitment: rpc.CommitmentFinalized,
	})
	if err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return Status{
				Confirmed:      false,
				MintSuccessful: false,
			}, nil
		}
		return Status{}, err
	}

	// Check Program ID.
	tx, err := res.Transaction.GetTransaction()
	if err != nil {
		return Status{}, fmt.Errorf("cannot get transaction: %w", err)
	}
	if len(tx.Message.Instructions) != 1 {
		return Status{}, nil
	}
	programID, err := tx.ResolveProgramIDIndex(tx.Message.Instructions[0].ProgramIDIndex)
	if err != nil {
		return Status{}, fmt.Errorf("cannot resolve program ID: %w", err)
	}
	if !programID.Equals(m.MinterProgram) {
		return Status{}, nil
	}

	// Check that transaction executed successfully.
	if res.Meta == nil {
		return Status{}, fmt.Errorf("nil meta")
	}
	if res.Meta.Err != nil {
		return Status{
			Confirmed:      true,
			MintSuccessful: false,
		}, nil
	}
	if res.BlockTime == nil {
		return Status{}, fmt.Errorf("nil block time")
	}
	return Status{
		Confirmed:        true,
		MintSuccessful:   true,
		ConfirmationTime: res.BlockTime.Time(),
	}, nil
}

func (m *minter) signTx(ctx context.Context, instruction solana.Instruction) (solana.Signature, *solana.Transaction, error) {
	recent, err := m.rpc.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("cannot get recent blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		recent.Value.Blockhash,
		solana.TransactionPayer(m.key.PublicKey()),
	)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("cannot create transaction: %w", err)
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if m.key.PublicKey().Equals(key) {
			return &m.key
		}
		return nil
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("cannot sign: %w", err)
	}
	return tx.Signatures[0], tx, nil
}
