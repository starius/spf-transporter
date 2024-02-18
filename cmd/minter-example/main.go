// In this example, the program will mint tokens on devnet using Minter Program.
// It will send those coins to the provided address.
// Note that Associated Token Address should exist for this address.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/gagliardetto/solana-go"
	solanatoken "gitlab.com/scpcorp/spf-transporter/solana"
)

const (
	actionSupplyInfo         = "supply-info"
	actionInitializeAirdrop  = "initialize-airdrop"
	actionMintInitialTranche = "mint-initial-tranche"
	actionMintEmission       = "mint-emission"
	//actionMintAirdrop        = "mint-airdrop"
)

type minter interface {
	InitializeAirdropTx(ctx context.Context, maxSupply int) (solana.Signature, *solana.Transaction, error)
	CheckWallet(ctx context.Context, walletAddr solana.PublicKey, amount uint64, skipWalletAccountCheck bool) error
	MintInitialTrancheTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error)
	MintEmissionTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error)
	//MintAirdropTx(ctx context.Context, walletAddr solana.PublicKey, amount, minterSupply uint64) (solana.Signature, *solana.Transaction, error)
	SendAndConfirm(ctx context.Context, tx *solana.Transaction) error
	SupplyInfo(ctx context.Context) (solanatoken.SupplyInfo, error)
	TxStatus(ctx context.Context, sig solana.Signature) (solanatoken.Status, error)
}

func usage() {
	fmt.Println("Usage: go run ./cmd/minter-example/main.go <action> [<parameters>]")
	fmt.Println("Actions:")
	fmt.Println("  supply-info")
	fmt.Println("  initialize-airdrop <max-supply>")
	fmt.Println("  mint-initial-tranche <wallet-address> <amount> <initial-tranche-supply>")
	fmt.Println("  mint-emission <wallet-address> <amount> <emission-supply>")
	fmt.Println("  mint-airdrop <wallet-address> <amount> <airdrop-supply>")
	os.Exit(1)
}

func main() {
	transporterKeyPath := os.Getenv("TRANSPORTER_KEY_PATH")
	if transporterKeyPath == "" {
		log.Fatal("TRANSPORTER_KEY_PATH undefined\n")
	}

	heliusApiKey := os.Getenv("HELIUS_API_KEY")
	if heliusApiKey == "" {
		log.Fatal("HELIUS_API_KEY undefined\n")
	}

	if len(os.Args) < 2 {
		usage()
	}

	m, err := solanatoken.NewMinter(context.Background(), solanatoken.NewMainNetConfig(transporterKeyPath, heliusApiKey))
	if err != nil {
		log.Fatalf("Cannot create minter: %s\n", err)
	}

	actionStr := os.Args[1]
	switch actionStr {
	case actionSupplyInfo:
		printSupplyInfo(m)
		return
	case actionInitializeAirdrop:
		maxSupplyStr := os.Args[2]
		maxSupply, err := strconv.Atoi(maxSupplyStr)
		if err != nil {
			log.Fatalf("Cannot parse amount into number '%s': %v", maxSupplyStr, err)
		}
		doInitializeAirdrop(m, maxSupply)
		return
	case actionMintInitialTranche:
	case actionMintEmission:
	//case actionMintAirdrop:
	default:
		log.Fatalf("Bad action: %s", actionStr)
	}

	walletStr := os.Args[2]
	walletAddr, err := solana.PublicKeyFromBase58(walletStr)
	if err != nil {
		log.Fatalf("Bad address: %v", err)
	}

	amountStr := os.Args[3]
	amount, err := strconv.Atoi(amountStr)
	if err != nil {
		log.Fatalf("Cannot parse amount into number '%s': %v", amountStr, err)
	}

	supplyStr := os.Args[4]
	supply, err := strconv.Atoi(supplyStr)
	if err != nil {
		log.Fatalf("Cannot parse total minted into number '%s': %v", supplyStr, err)
	}

	doMint(m, actionStr, walletAddr, uint64(amount), uint64(supply))
}

func printSupplyInfo(m minter) {
	supplyInfo, err := m.SupplyInfo(context.Background())
	if err != nil {
		log.Fatalf("Cannot get supply info: %v", err)
	}
	fmt.Printf("SupplyInfo: %+v\n", supplyInfo)
}

func doInitializeAirdrop(m minter, amount int) {
	sig, tx, err := m.InitializeAirdropTx(context.Background(), amount)
	if err != nil {
		log.Fatalf("Cannot build tx: %v", err)
	}
	fmt.Printf("Transaction: %s\n", sig)

	err = m.SendAndConfirm(context.Background(), tx)
	if err != nil {
		log.Fatalf("Cannot send: %v", err)
	}

	printSupplyInfo(m)
}

func doMint(m minter, action string, walletAddr solana.PublicKey, amount, supply uint64) {
	var sig solana.Signature
	var tx *solana.Transaction
	var err error
	switch action {
	case actionMintInitialTranche:
		sig, tx, err = m.MintInitialTrancheTx(context.Background(), walletAddr, amount, supply)
	case actionMintEmission:
		sig, tx, err = m.MintEmissionTx(context.Background(), walletAddr, amount, supply)
	//case actionMintAirdrop:
	//	sig, tx, err = m.MintAirdropTx(context.Background(), walletAddr, amount, supply)
	default:
		log.Fatalf("Bad mint type: %s", action)
	}
	if err != nil {
		log.Fatalf("Cannot build tx: %v", err)
	}
	fmt.Printf("Transaction: %s\n", sig)

	err = m.SendAndConfirm(context.Background(), tx)
	if err != nil {
		log.Fatalf("Cannot send: %v", err)
	}

	printSupplyInfo(m)
}
