package consensus

import (
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"gitlab.com/scpcorp/ScPrime/build"
	"gitlab.com/scpcorp/ScPrime/modules"
	"gitlab.com/scpcorp/ScPrime/types"
)

var (
	errApplySiafundPoolDiffMismatch  = errors.New("committing a siafund pool diff with an invalid 'previous' field")
	errDiffsNotGenerated             = errors.New("applying diff set before generating errors")
	errInvalidSuccessor              = errors.New("generating diffs for a block that's an invalid successsor to the current block")
	errNegativePoolAdjustment        = errors.New("committing a siafund pool diff with a negative adjustment")
	errNonApplySiafundPoolDiff       = errors.New("committing a siafund pool diff that doesn't have the 'apply' direction")
	errRevertSiafundPoolDiffMismatch = errors.New("committing a siafund pool diff with an invalid 'adjusted' field")
	errWrongAppliedDiffSet           = errors.New("applying a diff set that isn't the current block")
	errWrongRevertDiffSet            = errors.New("reverting a diff set that isn't the current block")
)

// commitDiffSetSanity performs a series of sanity checks before committing a
// diff set.
func commitDiffSetSanity(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	// This function is purely sanity checks.
	if !build.DEBUG {
		return
	}

	// Diffs should have already been generated for this node.
	if !pb.DiffsGenerated {
		panic(errDiffsNotGenerated)
	}

	// Current node must be the input node's parent if applying, and
	// current node must be the input node if reverting.
	if dir == modules.DiffApply {
		parent, err := getBlockMap(tx, pb.Block.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if parent.Block.ID() != currentBlockID(tx) {
			panic(errWrongAppliedDiffSet)
		}
	} else {
		if pb.Block.ID() != currentBlockID(tx) {
			panic(errWrongRevertDiffSet)
		}
	}
}

// commitSiacoinOutputDiff applies or reverts a SiacoinOutputDiff.
func commitSiacoinOutputDiff(tx *bolt.Tx, scod modules.SiacoinOutputDiff, dir modules.DiffDirection) {
	if scod.Direction == dir {
		addSiacoinOutput(tx, scod.ID, scod.SiacoinOutput)
	} else {
		removeSiacoinOutput(tx, scod.ID)
	}
}

// commitFileContractDiff applies or reverts a FileContractDiff.
func commitFileContractDiff(tx *bolt.Tx, fcd modules.FileContractDiff, dir modules.DiffDirection) {
	if fcd.Direction == dir {
		addFileContract(tx, fcd.ID, fcd.FileContract)
	} else {
		removeFileContract(tx, fcd.ID)
	}
}

// commitFileContractOwnerDiff applies or reverts a FileContractOwnerDiff.
func commitFileContractOwnerDiff(tx *bolt.Tx, fcod modules.FileContractOwnerDiff, dir modules.DiffDirection) {
	if fcod.Direction == dir {
		addFileContractRange(tx, fcod.Owners, FileContractRange{Start: fcod.StartHeight, End: fcod.EndHeight})
		addFileContractOwnership(tx, fcod.ID, FileContractOwnership{Owners: fcod.Owners, Start: fcod.StartHeight})
	} else {
		removeFileContractRange(tx, fcod.Owners, FileContractRange{Start: fcod.StartHeight, End: fcod.EndHeight})
		removeFileContractOwnership(tx, fcod.ID)
	}
}

// commitSiafundOutputDiff applies or reverts a Siafund output diff.
func commitSiafundOutputDiff(tx *bolt.Tx, sfod modules.SiafundOutputDiff, dir modules.DiffDirection) {
	if sfod.Direction == dir {
		addSiafundOutput(tx, sfod.ID, sfod.SiafundOutput)
	} else {
		removeSiafundOutput(tx, sfod.ID)
	}
}

// commitDelayedSiacoinOutputDiff applies or reverts a delayedSiacoinOutputDiff.
func commitDelayedSiacoinOutputDiff(tx *bolt.Tx, dscod modules.DelayedSiacoinOutputDiff, dir modules.DiffDirection) {
	if dscod.Direction == dir {
		addDSCO(tx, dscod.MaturityHeight, dscod.ID, dscod.SiacoinOutput)
	} else {
		removeDSCO(tx, dscod.MaturityHeight, dscod.ID)
	}
}

// commitSiafundPoolDiff applies or reverts a SiafundPoolDiff.
func commitSiafundPoolDiff(tx *bolt.Tx, sfpd modules.SiafundPoolDiff, dir modules.DiffDirection) {
	// Sanity check - siafund pool should only ever increase.
	if build.DEBUG {
		if sfpd.Adjusted.Cmp(sfpd.Previous) < 0 {
			panic(errNegativePoolAdjustment)
		}
		if sfpd.Direction != modules.DiffApply {
			panic(errNonApplySiafundPoolDiff)
		}
	}

	// Get current block height.
	blockchainHeight := blockHeight(tx)

	if dir == modules.DiffApply {
		// Sanity check - sfpd.Previous should equal the current siafund pool.
		if build.DEBUG && !getSiafundPool(tx).Equals(sfpd.Previous) {
			panic(errApplySiafundPoolDiffMismatch)
		}
		setSiafundPool(tx, sfpd.Adjusted)
		setSiafundHistoricalPool(tx, sfpd.Adjusted, blockchainHeight+1)
	} else {
		// Sanity check - sfpd.Adjusted should equal the current siafund pool.
		if build.DEBUG && !getSiafundPool(tx).Equals(sfpd.Adjusted) {
			panic(errRevertSiafundPoolDiffMismatch)
		}
		setSiafundPool(tx, sfpd.Previous)
		removeSiafundHistoricalPool(tx, blockchainHeight)
	}
}

// commitSiafundBDiff applies or reverts a SiafundBDiff.
func commitSiafundBDiff(tx *bolt.Tx, sfbd modules.SiafundBDiff, dir modules.DiffDirection) {
	if sfbd.Direction == dir {
		addSiafundBOutput(tx, sfbd.ID)
	} else {
		removeSiafundBOutput(tx, sfbd.ID)
	}
}

// createUpcomingDelayeOutputdMaps creates the delayed siacoin output maps that
// will be used when applying delayed siacoin outputs in the diff set.
func createUpcomingDelayedOutputMaps(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		createDSCOBucket(tx, pb.Height+types.MaturityDelay)
	} else if pb.Height >= types.MaturityDelay {
		createDSCOBucket(tx, pb.Height)
	}
}

// commitNodeDiffs commits all of the diffs in a block node.
func commitNodeDiffs(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		for _, scod := range pb.SiacoinOutputDiffs {
			commitSiacoinOutputDiff(tx, scod, dir)
		}
		for _, fcd := range pb.FileContractDiffs {
			commitFileContractDiff(tx, fcd, dir)
		}
		for _, fcd := range pb.FileContractOwnerDiffs {
			commitFileContractOwnerDiff(tx, fcd, dir)
		}
		for _, sfod := range pb.SiafundOutputDiffs {
			commitSiafundOutputDiff(tx, sfod, dir)
		}
		for _, dscod := range pb.DelayedSiacoinOutputDiffs {
			commitDelayedSiacoinOutputDiff(tx, dscod, dir)
		}
		for _, sfpd := range pb.SiafundPoolDiffs {
			commitSiafundPoolDiff(tx, sfpd, dir)
		}
		for _, sfbd := range pb.SiafundBDiffs {
			commitSiafundBDiff(tx, sfbd, dir)
		}
	} else {
		for i := len(pb.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			commitSiacoinOutputDiff(tx, pb.SiacoinOutputDiffs[i], dir)
		}
		for i := len(pb.FileContractDiffs) - 1; i >= 0; i-- {
			commitFileContractDiff(tx, pb.FileContractDiffs[i], dir)
		}
		for i := len(pb.FileContractOwnerDiffs) - 1; i >= 0; i-- {
			commitFileContractOwnerDiff(tx, pb.FileContractOwnerDiffs[i], dir)
		}
		for i := len(pb.SiafundOutputDiffs) - 1; i >= 0; i-- {
			commitSiafundOutputDiff(tx, pb.SiafundOutputDiffs[i], dir)
		}
		for i := len(pb.DelayedSiacoinOutputDiffs) - 1; i >= 0; i-- {
			commitDelayedSiacoinOutputDiff(tx, pb.DelayedSiacoinOutputDiffs[i], dir)
		}
		for i := len(pb.SiafundPoolDiffs) - 1; i >= 0; i-- {
			commitSiafundPoolDiff(tx, pb.SiafundPoolDiffs[i], dir)
		}
		for i := len(pb.SiafundBDiffs) - 1; i >= 0; i-- {
			commitSiafundBDiff(tx, pb.SiafundBDiffs[i], dir)
		}
	}
}

// deleteObsoleteDelayedOutputMaps deletes the delayed siacoin output maps that
// are no longer in use.
func deleteObsoleteDelayedOutputMaps(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	// There are no outputs that mature in the first MaturityDelay blocks.
	if dir == modules.DiffApply && pb.Height >= types.MaturityDelay {
		deleteDSCOBucket(tx, pb.Height)
	} else if dir == modules.DiffRevert {
		deleteDSCOBucket(tx, pb.Height+types.MaturityDelay)
	}
}

// updateSiafundPoolHistory updates historical SPF pool values.
func updateSiafundPoolHistory(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		setSiafundHistoricalPool(tx, getSiafundPool(tx), pb.Height)
	} else {
		removeSiafundHistoricalPool(tx, pb.Height)
	}
}

// updateCurrentPath updates the current path after applying a diff set.
func updateCurrentPath(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	// Update the current path.
	if dir == modules.DiffApply {
		pushPath(tx, pb.Block.ID())
	} else {
		popPath(tx)
	}
}

// commitDiffSet applies or reverts the diffs in a blockNode.
func commitDiffSet(tx *bolt.Tx, pb *processedBlockV2, dir modules.DiffDirection) {
	// Sanity checks - there are a few so they were moved to another function.
	if build.DEBUG {
		commitDiffSetSanity(tx, pb, dir)
	}

	createUpcomingDelayedOutputMaps(tx, pb, dir)
	commitNodeDiffs(tx, pb, dir)
	deleteObsoleteDelayedOutputMaps(tx, pb, dir)
	if types.Fork2022 && pb.Height >= types.SpfPoolHistoryHardforkHeight {
		updateSiafundPoolHistory(tx, pb, dir)
	}
	updateCurrentPath(tx, pb, dir)
}

// SPF pool history hardfork fixes bug introduced in Fork2022.
// It iterates pool-by-height values and inserts them if needed.
// The bug is, some blocks don't have pool values because
// pool values were inserted from SiafundPoolDiffs which don't exist
// when pool value doesn't change.
func applySpfPoolHistoryHardfork(tx *bolt.Tx) (err error) {
	currentPool := getSiafundHardforkPool(tx, types.Fork2022Height)
	var heightsWithoutPool []types.BlockHeight
	for height := types.Fork2022Height; height <= types.SpfPoolHistoryHardforkHeight; height++ {
		pool, err := getSiafundPoolAtHeight(tx, height)
		if err == errNilItem {
			// Missing value because it hasn't changed, need to insert the previous one.
			heightsWithoutPool = append(heightsWithoutPool, height)
			setSiafundHistoricalPool(tx, currentPool, height)
			continue
		} else if err != nil {
			return fmt.Errorf("failed to get pool at height %d: %w", height, err)
		}
		currentPool = pool
	}
	storeHeightsWithoutPools(tx, heightsWithoutPool)
	return nil
}

func revertSpfPoolHistoryHardfork(tx *bolt.Tx) {
	heightsToRemove := heightsWithoutPools(tx)
	for _, h := range heightsToRemove {
		removeSiafundHistoricalPool(tx, h)
	}
}

// generateAndApplyDiff will verify the block and then integrate it into the
// consensus state. These two actions must happen at the same time because
// transactions are allowed to depend on each other. We can't be sure that a
// transaction is valid unless we have applied all of the previous transactions
// in the block, which means we need to apply while we verify.
func generateAndApplyDiff(tx *bolt.Tx, pb *processedBlockV2) error {
	// Sanity check - the block being applied should have the current block as
	// a parent.
	if build.DEBUG && pb.Block.ParentID != currentBlockID(tx) {
		panic(errInvalidSuccessor)
	}

	// Create the bucket to hold all of the delayed siacoin outputs created by
	// transactions this block. Needs to happen before any transactions are
	// applied.
	createDSCOBucket(tx, pb.Height+types.MaturityDelay)

	// Validate and apply each transaction in the block. They cannot be
	// validated all at once because some transactions may not be valid until
	// previous transactions have been applied.
	for _, txn := range pb.Block.Transactions {
		err := validTransaction(tx, txn)
		if err != nil {
			return err
		}
		applyTransaction(tx, pb, txn)
	}

	// After all of the transactions have been applied, 'maintenance' is
	// applied on the block. This includes adding any outputs that have reached
	// maturity, applying any contracts with missed storage proofs, and adding
	// the miner payouts to the list of delayed outputs.
	applyMaintenance(tx, pb)

	// Fx the scprimefund allocation
	if pb.Height == types.SpfAirdropHeight {
		// Remove Genesis Siafunds
		for i, siafundOutput := range types.GenesisBlock.Transactions[1].SiafundOutputs {
			sfid := types.GenesisBlock.Transactions[1].SiafundOutputID(uint64(i))
			sfod := modules.SiafundOutputDiff{
				Direction: modules.DiffRevert,
				ID:        sfid,
				SiafundOutput: types.SiafundOutput{
					Value:      types.NewCurrency64(0),
					UnlockHash: siafundOutput.UnlockHash,
				},
			}
			pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
			commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
			//removeSiafundOutput(tx, sfod.ID)
		}

		// Add ForkedGenesisSiafundAllocation
		for _, siafundOutput := range types.ForkedGenesisSiafundAllocation {
			sfid := types.SiafundOutputID(siafundOutput.UnlockHash)
			sfod := modules.SiafundOutputDiff{
				Direction:     modules.DiffApply,
				ID:            sfid,
				SiafundOutput: siafundOutput,
			}
			pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
			commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
			//addSiafundOutput(tx, sfod.ID, sfod.SiafundOutput)
			//dbAddSiafundOutput(types.SiafundOutputID{}, siafundOutput)
		}
	}

	if types.IsSpfHardfork(pb.Height) {
		// Perform SPF hardfork changes.
		// At first, save current siafund pool.
		currentPool := getSiafundPool(tx)
		setSiafundHardforkPool(tx, currentPool, pb.Height)
		// Now apply new outputs.
		for _, siafundOutput := range types.SiafundHardforkAllocation[pb.Height] {
			sfid := types.SiafundOutputID(siafundOutput.UnlockHash)
			siafundOutput.ClaimStart = getSiafundPool(tx)
			sfod := modules.SiafundOutputDiff{
				Direction:     modules.DiffApply,
				ID:            sfid,
				SiafundOutput: siafundOutput,
			}
			pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
			commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
			// If we're at Fork2022, mark all new siafund outputs as SPF-B.
			if pb.Height == types.Fork2022Height {
				sfbd := modules.SiafundBDiff{
					Direction: modules.DiffApply,
					ID:        sfid,
				}
				pb.SiafundBDiffs = append(pb.SiafundBDiffs, sfbd)
				commitSiafundBDiff(tx, sfbd, modules.DiffApply)
			}
		}
	}

	// DiffsGenerated are only set to true after the block has been fully
	// validated and integrated. This is required to prevent later blocks from
	// being accepted on top of an invalid block - if the consensus set ever
	// forks over an invalid block, 'DiffsGenerated' will be set to 'false',
	// requiring validation to occur again. when 'DiffsGenerated' is set to
	// true, validation is skipped, therefore the flag should only be set to
	// true on fully validated blocks.
	pb.DiffsGenerated = true

	if types.Fork2022 && pb.Height >= types.SpfPoolHistoryHardforkHeight {
		updateSiafundPoolHistory(tx, pb, modules.DiffApply)
	}
	// Add the block to the current path and block map.
	updateCurrentPath(tx, pb, modules.DiffApply)

	// Sanity check preparation - set the consensus hash at this height so that
	// during reverting a check can be performed to assure consistency when
	// adding and removing blocks. Must happen after the block is added to the
	// path.
	if build.DEBUG {
		pb.ConsensusChecksum = consensusChecksum(tx)
	}

	addBlockMap(tx, pb)
	return nil
}
