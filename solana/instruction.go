package solana

import "encoding/binary"

// // https://gitlab.com/scpcorp/solana-token/-/blob/main/contracts/minter/src/instruction.rs
const (
	instructionNumInitializeAirdrop byte = iota
	instructionNumMintInitialTranche
	instructionNumMintEmission
	instructionNumMintAirdrop
)

type instructionInitializeAirdrop struct {
	rawMaxSupply uint64
}

func (i instructionInitializeAirdrop) InstructionData() []byte {
	instructionData := make([]byte, 0, 9)
	instructionData = append(instructionData, instructionNumInitializeAirdrop)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawMaxSupply)
	return instructionData
}

type instructionMintInitialTranche struct {
	rawAmount, rawInitialTrancheSupply uint64
}

func (i instructionMintInitialTranche) InstructionData() []byte {
	instructionData := make([]byte, 0, 17)
	instructionData = append(instructionData, instructionNumMintInitialTranche)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawAmount)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawInitialTrancheSupply)
	return instructionData
}

type instructionMintEmission struct {
	rawAmount, rawEmissionSupply uint64
}

func (i instructionMintEmission) InstructionData() []byte {
	instructionData := make([]byte, 0, 17)
	instructionData = append(instructionData, instructionNumMintEmission)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawAmount)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawEmissionSupply)
	return instructionData
}

type instructionMintAirdrop struct {
	rawAmount, rawAirdropSupply uint64
}

func (i instructionMintAirdrop) InstructionData() []byte {
	instructionData := make([]byte, 0, 17)
	instructionData = append(instructionData, instructionNumMintAirdrop)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawAmount)
	instructionData = binary.LittleEndian.AppendUint64(instructionData, i.rawAirdropSupply)
	return instructionData
}
