package consensus

import (
	"errors"
	"fmt"

	"gitlab.com/scpcorp/ScPrime/build"
	"gitlab.com/scpcorp/ScPrime/modules"
	"gitlab.com/scpcorp/ScPrime/types"

	bolt "go.etcd.io/bbolt"
)

var (
	errExternalRevert = errors.New("cannot revert to block outside of current path")
)

// backtrackToCurrentPath traces backwards from 'pb' until it reaches a block
// in the ConsensusSet's current path (the "common parent"). It returns the
// (inclusive) set of blocks between the common parent and 'pb', starting from
// the former.
func backtrackToCurrentPath(tx *bolt.Tx, pb *processedBlockV2) []*processedBlockV2 {
	path := []*processedBlockV2{pb}
	for {
		// Error is not checked in production code - an error can only indicate
		// that pb.Height > blockHeight(tx).
		currentPathID, err := getPath(tx, pb.Height)
		if currentPathID == pb.Block.ID() {
			break
		}
		// Sanity check - an error should only indicate that pb.Height >
		// blockHeight(tx).
		if build.DEBUG && err != nil && pb.Height <= blockHeight(tx) {
			panic(err)
		}

		// Prepend the next block to the list of blocks leading from the
		// current path to the input block.
		pb, err = getBlockMap(tx, pb.Block.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		path = append([]*processedBlockV2{pb}, path...)
	}
	return path
}

// revertToBlock will revert blocks from the ConsensusSet's current path until
// 'pb' is the current block. Blocks are returned in the order that they were
// reverted.  'pb' is not reverted.
func (cs *ConsensusSet) revertToBlock(tx *bolt.Tx, pb *processedBlockV2) (revertedBlocks []*processedBlockV2) {
	// Sanity check - make sure that pb is in the current path.
	currentPathID, err := getPath(tx, pb.Height)
	if err != nil || currentPathID != pb.Block.ID() {
		if build.DEBUG {
			panic(errExternalRevert) // needs to be panic for TestRevertToNode
		} else {
			build.Critical(errExternalRevert)
		}
	}

	// Rewind blocks until 'pb' is the current block.
	for currentBlockID(tx) != pb.Block.ID() {
		height := blockHeight(tx)
		if types.IsSpfHardfork(height) {
			// We are reverting one of the SPF Emission Hardfork blocks, we need to
			// reset SPF hardfork siafund pool.
			setSiafundHardforkPool(tx, types.ZeroCurrency, height)
		}
		if types.IsSpfPoolHistoryHardfork(height) {
			// We are reverting SPF pool history hardfork,
			// it requires manual rollback procedure.
			revertSpfPoolHistoryHardfork(tx)
		}
		block := currentProcessedBlock(tx)
		commitDiffSet(tx, block, modules.DiffRevert)
		revertedBlocks = append(revertedBlocks, block)

		// Sanity check - after removing a block, check that the consensus set
		// has maintained consistency.
		if build.Release == "testing" {
			cs.checkConsistency(tx)
		} /* else {
			cs.maybeCheckConsistency(tx)
		} */
	}
	return revertedBlocks
}

// applyUntilBlock will successively apply the blocks between the consensus
// set's current path and 'pb'.
func (cs *ConsensusSet) applyUntilBlock(tx *bolt.Tx, pb *processedBlockV2) (appliedBlocks []*processedBlockV2, err error) {
	// Backtrack to the common parent of 'bn' and current path and then apply the new blocks.
	newPath := backtrackToCurrentPath(tx, pb)
	for _, block := range newPath[1:] {
		// If the diffs for this block have already been generated, apply diffs
		// directly instead of generating them. This is much faster.
		if block.DiffsGenerated {
			commitDiffSet(tx, block, modules.DiffApply)
		} else {
			err := generateAndApplyDiff(tx, block)
			if err != nil {
				// Mark the block as invalid.
				cs.dosBlocks[block.Block.ID()] = struct{}{}
				return nil, err
			}
		}
		if types.IsSpfPoolHistoryHardfork(block.Height) {
			// At SiafundPoolHistoryHardforkHeight a correction is applied to add
			// all missing values so that for every block after Fork2022Height there is
			// Siafund pool value stored. Before this, when there are no SiafundPoolDiffs
			// in the block, no pool values were stored which lead to mistakes
			// in SPF-B claim calculation.
			// This change is a hardfork, because claim values for same SPF-B outputs
			// might differ from what the previous version would produce.
			if err := applySpfPoolHistoryHardfork(tx); err != nil {
				return nil, fmt.Errorf("failed to add missing values to SPF pool history: %w", err)
			}
		}

		appliedBlocks = append(appliedBlocks, block)

		// Sanity check - after applying a block, check that the consensus set
		// has maintained consistency.
		if build.Release == "testing" {
			cs.checkConsistency(tx)
		} /* else {
			cs.maybeCheckConsistency(tx)
		} */
	}
	return appliedBlocks, nil
}

// forkBlockchain will move the consensus set onto the 'newBlock' fork. An
// error will be returned if any of the blocks applied in the transition are
// found to be invalid. forkBlockchain is atomic; the ConsensusSet is only
// updated if the function returns nil.
func (cs *ConsensusSet) forkBlockchain(tx *bolt.Tx, newBlock *processedBlockV2) (revertedBlocks, appliedBlocks []*processedBlockV2, err error) {
	commonParent := backtrackToCurrentPath(tx, newBlock)[0]
	revertedBlocks = cs.revertToBlock(tx, commonParent)
	appliedBlocks, err = cs.applyUntilBlock(tx, newBlock)
	if err != nil {
		return nil, nil, err
	}
	return revertedBlocks, appliedBlocks, nil
}
