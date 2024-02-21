package modules

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"gitlab.com/scpcorp/ScPrime/crypto"
	"gitlab.com/scpcorp/ScPrime/types"
)

type (
	// ProgramBuilder is a helper type to easily create programs and compute
	// their cost.
	ProgramBuilder struct {
		staticDuration types.BlockHeight
		staticPT       *RPCPriceTable

		readonly    bool
		program     Program
		programData *bytes.Buffer

		// Cost related fields.
		executionCost     types.Currency
		additionalStorage types.Currency
		riskedCollateral  types.Currency
		usedMemory        uint64
	}
)

// NewProgramBuilder creates an empty program builder.
func NewProgramBuilder(pt *RPCPriceTable, duration types.BlockHeight) *ProgramBuilder {
	pb := &ProgramBuilder{
		programData:    new(bytes.Buffer),
		readonly:       true, // every program starts readonly
		staticDuration: duration,
		staticPT:       pt,
		usedMemory:     MDMInitMemory(),
	}
	return pb
}

// AddAppendInstruction adds an Append instruction to the program.
func (pb *ProgramBuilder) AddAppendInstruction(data []byte, merkleProof bool) error {
	if uint64(len(data)) != SectorSize {
		return fmt.Errorf("expected appended data to have size %v but was %v", SectorSize, len(data))
	}
	// Compute the argument offsets.
	dataOffset := uint64(pb.programData.Len())
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, data)
	// Create the instruction.
	i := NewAppendInstruction(dataOffset, merkleProof)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMAppendCollateral(pb.staticPT)
	cost, refund := MDMAppendCost(pb.staticPT, pb.staticDuration)
	memory := MDMAppendMemory()
	time := uint64(MDMTimeAppend)
	pb.addInstruction(collateral, cost, refund, memory, time)
	pb.readonly = false
	return nil
}

// AddDropSectorsInstruction adds a DropSectors instruction to the program.
func (pb *ProgramBuilder) AddDropSectorsInstruction(numSectors uint64, merkleProof bool) {
	// Compute the argument offsets.
	numSectorsOffset := uint64(pb.programData.Len())
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, numSectors)
	// Create the instruction.
	i := NewDropSectorsInstruction(numSectorsOffset, merkleProof)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMDropSectorsCollateral()
	cost := MDMDropSectorsCost(pb.staticPT, numSectors)
	memory := MDMDropSectorsMemory()
	time := MDMDropSectorsTime(numSectors)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
	pb.readonly = false
}

// AddHasSectorInstruction adds a HasSector instruction to the program.
func (pb *ProgramBuilder) AddHasSectorInstruction(merkleRoot crypto.Hash) {
	// Compute the argument offsets.
	merkleRootOffset := uint64(pb.programData.Len())
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, merkleRoot[:])
	// Create the instruction.
	i := NewHasSectorInstruction(merkleRootOffset)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMHasSectorCollateral()
	cost := MDMHasSectorCost(pb.staticPT)
	memory := MDMHasSectorMemory()
	time := uint64(MDMTimeHasSector)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
}

// AddReadOffsetInstruction adds a ReadOffset instruction to the program.
func (pb *ProgramBuilder) AddReadOffsetInstruction(length, offset uint64, merkleProof bool) {
	// Compute the argument offsets.
	lengthOffset := uint64(pb.programData.Len())
	offsetOffset := lengthOffset + 8
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, length)
	binary.Write(pb.programData, binary.LittleEndian, offset)
	// Create the instruction.
	i := NewReadOffsetInstruction(lengthOffset, offsetOffset, merkleProof)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMReadCollateral()
	cost := MDMReadCost(pb.staticPT, length)
	memory := MDMReadMemory()
	time := uint64(MDMTimeReadOffset)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
}

// AddReadSectorInstruction adds a ReadSector instruction to the program.
func (pb *ProgramBuilder) AddReadSectorInstruction(length, offset uint64, merkleRoot crypto.Hash, merkleProof bool) {
	// Compute the argument offsets.
	lengthOffset := uint64(pb.programData.Len())
	offsetOffset := lengthOffset + 8
	merkleRootOffset := offsetOffset + 8
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, length)
	binary.Write(pb.programData, binary.LittleEndian, offset)
	binary.Write(pb.programData, binary.LittleEndian, merkleRoot[:])
	// Create the instruction.
	i := NewReadSectorInstruction(lengthOffset, offsetOffset, merkleRootOffset, merkleProof)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMReadCollateral()
	cost := MDMReadCost(pb.staticPT, length)
	memory := MDMReadMemory()
	time := uint64(MDMTimeReadSector)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
}

// AddRevisionInstruction adds a Revision instruction to the program.
func (pb *ProgramBuilder) AddRevisionInstruction() {
	// Compute the argument offsets.
	merkleRootOffset := uint64(pb.programData.Len())
	// Create the instruction.
	i := NewRevisionInstruction(merkleRootOffset)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMRevisionCollateral()
	cost := MDMRevisionCost(pb.staticPT)
	memory := MDMRevisionMemory()
	time := uint64(MDMTimeRevision)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
}

// AddSwapSectorInstruction adds a SwapSector instruction to the program.
func (pb *ProgramBuilder) AddSwapSectorInstruction(sector1Idx, sector2Idx uint64, merkleProof bool) {
	// Compute the argument offsets.
	sector1Offset := uint64(pb.programData.Len())
	sector2Offset := sector1Offset + 8
	// Extend the programData.
	binary.Write(pb.programData, binary.LittleEndian, sector1Idx)
	binary.Write(pb.programData, binary.LittleEndian, sector2Idx)
	// Create the instruction.
	i := NewSwapSectorInstruction(sector1Offset, sector2Offset, merkleProof)
	// Append instruction
	pb.program = append(pb.program, i)
	// Update cost, collateral and memory usage.
	collateral := MDMSwapSectorCollateral()
	cost := MDMSwapSectorCost(pb.staticPT)
	memory := MDMSwapSectorMemory()
	time := uint64(MDMTimeSwapSector)
	pb.addInstruction(collateral, cost, types.ZeroCurrency, memory, time)
	pb.readonly = false
}

// Cost returns the current cost of the program being built by the builder. If
// 'finalized' is 'true', the memory cost of finalizing the program is included.
func (pb *ProgramBuilder) Cost(finalized bool) (cost, storage, collateral types.Currency) {
	// Calculate the init cost.
	cost = MDMInitCost(pb.staticPT, uint64(pb.programData.Len()), uint64(len(pb.program)))

	// Add the cost of the added instructions
	cost = cost.Add(pb.executionCost)

	// Add the cost of finalizing the program.
	if !pb.readonly && finalized {
		cost = cost.Add(MDMMemoryCost(pb.staticPT, pb.usedMemory, MDMTimeCommit))
	}
	return cost, pb.additionalStorage, pb.riskedCollateral
}

// Program returns the built program and programData.
func (pb *ProgramBuilder) Program() (Program, ProgramData) {
	return pb.program, pb.programData.Bytes()
}

// addInstruction adds the collateral, cost, refund and memory cost of an
// instruction to the builder's state.
func (pb *ProgramBuilder) addInstruction(collateral, cost, storage types.Currency, memory, time uint64) {
	// Update collateral
	pb.riskedCollateral = pb.riskedCollateral.Add(collateral)
	// Update memory and memory cost.
	pb.usedMemory += memory
	memoryCost := MDMMemoryCost(pb.staticPT, pb.usedMemory, time)
	pb.executionCost = pb.executionCost.Add(memoryCost)
	// Update execution cost and storage.
	pb.executionCost = pb.executionCost.Add(cost)
	pb.additionalStorage = pb.additionalStorage.Add(storage)
}

// NewAppendInstruction creates an Instruction from arguments.
func NewAppendInstruction(dataOffset uint64, merkleProof bool) Instruction {
	i := Instruction{
		Specifier: SpecifierAppend,
		Args:      make([]byte, RPCIAppendLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], dataOffset)
	if merkleProof {
		i.Args[8] = 1
	}
	return i
}

// NewDropSectorsInstruction creates an Instruction from arguments.
func NewDropSectorsInstruction(numSectorsOffset uint64, merkleProof bool) Instruction {
	i := Instruction{
		Specifier: SpecifierDropSectors,
		Args:      make([]byte, RPCIDropSectorsLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], numSectorsOffset)
	if merkleProof {
		i.Args[8] = 1
	}
	return i
}

// NewHasSectorInstruction creates a modules.Instruction from arguments.
func NewHasSectorInstruction(merkleRootOffset uint64) Instruction {
	i := Instruction{
		Specifier: SpecifierHasSector,
		Args:      make([]byte, RPCIHasSectorLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], merkleRootOffset)
	return i
}

// NewReadOffsetInstruction creates a modules.Instruction from arguments.
func NewReadOffsetInstruction(lengthOffset, offsetOffset uint64, merkleProof bool) Instruction {
	i := Instruction{
		Specifier: SpecifierReadOffset,
		Args:      make([]byte, RPCIReadOffsetLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], offsetOffset)
	binary.LittleEndian.PutUint64(i.Args[8:16], lengthOffset)
	if merkleProof {
		i.Args[16] = 1
	}
	return i
}

// NewReadSectorInstruction creates a modules.Instruction from arguments.
func NewReadSectorInstruction(lengthOffset, offsetOffset, merkleRootOffset uint64, merkleProof bool) Instruction {
	i := Instruction{
		Specifier: SpecifierReadSector,
		Args:      make([]byte, RPCIReadSectorLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], merkleRootOffset)
	binary.LittleEndian.PutUint64(i.Args[8:16], offsetOffset)
	binary.LittleEndian.PutUint64(i.Args[16:24], lengthOffset)
	if merkleProof {
		i.Args[24] = 1
	}
	return i
}

// NewSwapSectorInstruction creates a modules.Instruction from arguments.
func NewSwapSectorInstruction(sector1Offset, sector2Offset uint64, merkleProof bool) Instruction {
	i := Instruction{
		Specifier: SpecifierSwapSector,
		Args:      make([]byte, RPCISwapSectorLen),
	}
	binary.LittleEndian.PutUint64(i.Args[:8], sector1Offset)
	binary.LittleEndian.PutUint64(i.Args[8:16], sector2Offset)
	if merkleProof {
		i.Args[16] = 1
	}
	return i
}

// NewRevisionInstruction creates a modules.Instruction from arguments.
func NewRevisionInstruction(merkleRootOffset uint64) Instruction {
	return Instruction{
		Specifier: SpecifierRevision,
		Args:      nil,
	}
}
