package solana

import (
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type minterConfig struct {
	// Cluster config.
	Cluster rpc.Cluster

	// Path to a solana-keygen file with transporter keypair.
	SolanaKeygenFile string

	// Minter program address.
	MinterProgram solana.PublicKey

	// Token Mint address.
	Token solana.PublicKey

	// Seed used to derive Mint Authority address.
	MintAuthoritySeed string

	// Seed used to derive Emission account address.
	MintingStateSeed string

	// Minimum amount of tokens that is enough for Minter Program to fund
	// recipient's associated token account.
	FundATAMinAmount uint64

	// Number of token decimals.
	Decimals int
}

func NewDevNetConfig(solanaKeygenFile, heliusApiKey string) minterConfig {
	return minterConfig{
		Cluster: rpc.Cluster{
			Name: "devnet",
			RPC:  "https://devnet.helius-rpc.com/?api-key=" + heliusApiKey,
			WS:   "ws://devnet.helius-rpc.com/?api-key=" + heliusApiKey,
		},
		SolanaKeygenFile:  solanaKeygenFile,
		MinterProgram:     solana.MustPublicKeyFromBase58("Hu2hnLX8vk9itUdBk1MzEFAeT9NqvyFz6mD2ak1Z976n"),
		Token:             solana.MustPublicKeyFromBase58("5zxeCQmKj7SPkaLNc1dZo5fzEtdQYses1dYgcCwW7b9V"),
		MintAuthoritySeed: "mint-authority",
		MintingStateSeed:  "minting-state3",
		FundATAMinAmount:  100,
		Decimals:          3,
	}
}

func NewMainNetConfig(solanaKeygenFile, heliusApiKey string) minterConfig {
	return minterConfig{
		Cluster: rpc.Cluster{
			Name: "mainnet-beta",
			RPC:  "https://mainnet.helius-rpc.com/?api-key=" + heliusApiKey,
			WS:   "ws://mainnet.helius-rpc.com/?api-key=" + heliusApiKey,
		},
		SolanaKeygenFile:  solanaKeygenFile,
		MinterProgram:     solana.MustPublicKeyFromBase58("LmFEsQUpHAV39wLYWaDJyny67t9Fk6zW3KNsHqW3Qup"),
		Token:             solana.MustPublicKeyFromBase58("GyuP7chtXSRB6erApifBxFvuTtz94x3zQo3JdWofBTgy"),
		MintAuthoritySeed: "mint-authority",
		MintingStateSeed:  "minting-state2",
		FundATAMinAmount:  100,
		Decimals:          3,
	}
}
