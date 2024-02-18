package consensus

// applytransaction.go handles applying a transaction to the consensus set.
// There is an assumption that the transaction has already been verified.

import (
	bolt "go.etcd.io/bbolt"

	"gitlab.com/scpcorp/ScPrime/build"
	"gitlab.com/scpcorp/ScPrime/modules"
	"gitlab.com/scpcorp/ScPrime/types"
)

// applySiacoinInputs takes all of the siacoin inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinInputs(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	// Remove all siacoin inputs from the unspent siacoin outputs list.
	for _, sci := range t.SiacoinInputs {
		sco, err := getSiacoinOutput(tx, sci.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sci.ParentID,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applySiacoinOutputs takes all of the siacoin outputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinOutputs(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	// Add all siacoin outputs to the unspent siacoin outputs list.
	for i, sco := range t.SiacoinOutputs {
		scoid := t.SiacoinOutputID(uint64(i))
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffApply,
			ID:            scoid,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applyFileContracts iterates through all of the file contracts in a
// transaction and applies them to the state, updating the diffs in the proccesed
// block.
func applyFileContracts(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	contractOwners := t.SponsorAddresses()
	for i, fc := range t.FileContracts {
		fcid := t.FileContractID(uint64(i))
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcid,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		// Get the portion of the contract that goes into the siafund pool and
		// add it to the siafund pool.
		sfp := getSiafundPool(tx)
		sfpd := modules.SiafundPoolDiff{
			Direction: modules.DiffApply,
			Previous:  sfp,
			Adjusted:  sfp.Add(types.Tax(blockHeight(tx), fc.Payout)),
		}
		pb.SiafundPoolDiffs = append(pb.SiafundPoolDiffs, sfpd)
		commitSiafundPoolDiff(tx, sfpd, modules.DiffApply)

		if pb.Height > types.Fork2022Height {
			// Add contract to its owners' history.
			fcod := modules.FileContractOwnerDiff{
				Direction:   modules.DiffApply,
				ID:          fcid,
				Owners:      contractOwners,
				StartHeight: pb.Height,
				EndHeight:   fc.WindowEnd,
			}
			pb.FileContractOwnerDiffs = append(pb.FileContractOwnerDiffs, fcod)
			commitFileContractOwnerDiff(tx, fcod, modules.DiffApply)
		}
	}
}

// updateContractEndHeight updates end height in contract owners' history.
func updateContractEndHeight(tx *bolt.Tx, pb *processedBlockV2, contractID types.FileContractID, prevHeight, newHeight types.BlockHeight) {
	contractOwnership, err := getFileContractOwnership(tx, contractID)
	if err == errNilItem {
		// May happen for contracts which already existed at Fork2022 point.
		// We don't account them for SPF-B and ignore them here.
		return
	} else if build.DEBUG && err != nil {
		panic(err)
	}

	if contractOwnership.Start <= types.Fork2022Height {
		// Don't do anything with contracts formed prior to Fork2022.
		return
	}

	// Delete existing record since end height changes.
	fcod := modules.FileContractOwnerDiff{
		Direction:   modules.DiffRevert,
		ID:          contractID,
		Owners:      contractOwnership.Owners,
		StartHeight: contractOwnership.Start,
		EndHeight:   prevHeight,
	}
	pb.FileContractOwnerDiffs = append(pb.FileContractOwnerDiffs, fcod)
	commitFileContractOwnerDiff(tx, fcod, modules.DiffApply)
	// Add new record with updated height.
	fcod = modules.FileContractOwnerDiff{
		Direction:   modules.DiffApply,
		ID:          contractID,
		Owners:      contractOwnership.Owners,
		StartHeight: contractOwnership.Start,
		EndHeight:   newHeight,
	}
	pb.FileContractOwnerDiffs = append(pb.FileContractOwnerDiffs, fcod)
	commitFileContractOwnerDiff(tx, fcod, modules.DiffApply)
}

// applyTxFileContractRevisions iterates through all of the file contract
// revisions in a transaction and applies them to the state, updating the diffs
// in the processed block.
func applyFileContractRevisions(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	for _, fcr := range t.FileContractRevisions {
		fc, err := getFileContract(tx, fcr.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add the diff to delete the old file contract.
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           fcr.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		// Add the diff to add the revised file contract.
		newFC := types.FileContract{
			FileSize:           fcr.NewFileSize,
			FileMerkleRoot:     fcr.NewFileMerkleRoot,
			WindowStart:        fcr.NewWindowStart,
			WindowEnd:          fcr.NewWindowEnd,
			Payout:             fc.Payout,
			ValidProofOutputs:  fcr.NewValidProofOutputs,
			MissedProofOutputs: fcr.NewMissedProofOutputs,
			UnlockHash:         fcr.NewUnlockHash,
			RevisionNumber:     fcr.NewRevisionNumber,
		}
		fcd = modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcr.ParentID,
			FileContract: newFC,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		if fcr.NewWindowEnd != fc.WindowEnd {
			updateContractEndHeight(tx, pb, fcr.ParentID, fc.WindowEnd, fcr.NewWindowEnd)
		}
	}
}

// applyTxStorageProofs iterates through all of the storage proofs in a
// transaction and applies them to the state, updating the diffs in the processed
// block.
func applyStorageProofs(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	for _, sp := range t.StorageProofs {
		fc, err := getFileContract(tx, sp.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add all of the outputs in the ValidProofOutputs of the contract.
		for i, vpo := range fc.ValidProofOutputs {
			spoid := sp.ParentID.StorageProofOutputID(types.ProofValid, uint64(i))
			dscod := modules.DelayedSiacoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             spoid,
				SiacoinOutput:  vpo,
				MaturityHeight: pb.Height + types.MaturityDelay,
			}
			pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
			commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		}

		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           sp.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTxSiafundInputs takes all of the siafund inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiafundInputs(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	for _, sfi := range t.SiafundInputs {
		// Calculate the volume of siacoins to put in the claim output.
		sfo, err := getSiafundOutput(tx, sfi.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		claim := siafundClaim(tx, sfi.ParentID, sfo)

		// Save zero claims before Fork2022 or any time if Fork2022 is not enabled.
		saveZeroClaimOutput := !types.Fork2022 || (pb.Height <= types.Fork2022Height)
		if !claim.ByOwner.IsZero() || saveZeroClaimOutput {
			// Add the claim output to the delayed set of outputs.
			sco := types.SiacoinOutput{
				Value:      claim.ByOwner,
				UnlockHash: sfi.ClaimUnlockHash,
			}
			sfoid := sfi.ParentID.SiaClaimOutputID()
			dscod := modules.DelayedSiacoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             sfoid,
				SiacoinOutput:  sco,
				MaturityHeight: pb.Height + types.MaturityDelay,
			}
			pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
			commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		}

		if !claim.Total.Equals(claim.ByOwner) {
			// Add another claim output for DevFund in case of SPF-B.
			sco := types.SiacoinOutput{
				Value:      types.SiafundBLostClaim(claim),
				UnlockHash: types.SiafundBLostClaimAddress,
			}
			sfoid := sfi.ParentID.SiaClaimSecondOutputID()
			dscod := modules.DelayedSiacoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             sfoid,
				SiacoinOutput:  sco,
				MaturityHeight: pb.Height + types.MaturityDelay,
			}
			pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
			commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		}

		// Create the siafund output diff and remove the output from the
		// consensus set.
		sfod := modules.SiafundOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sfi.ParentID,
			SiafundOutput: sfo,
		}
		pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
		commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
	}
}

func hasSiafundBInputs(tx *bolt.Tx, t types.Transaction) bool {
	for _, sfi := range t.SiafundInputs {
		isB := isSiafundBOutput(tx, sfi.ParentID)
		if isB {
			return true
		}
	}
	return false
}

// applySiafundOutput applies a siafund output to the consensus set.
func applySiafundOutputs(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	hasSiafundB := hasSiafundBInputs(tx, t)
	for i, sfo := range t.SiafundOutputs {
		sfoid := t.SiafundOutputID(uint64(i))
		sfo.ClaimStart = getSiafundPool(tx)
		sfod := modules.SiafundOutputDiff{
			Direction:     modules.DiffApply,
			ID:            sfoid,
			SiafundOutput: sfo,
		}
		pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
		commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
		if hasSiafundB {
			// Mark all outputs as SPF-B.
			sfbd := modules.SiafundBDiff{
				Direction: modules.DiffApply,
				ID:        sfoid,
			}
			pb.SiafundBDiffs = append(pb.SiafundBDiffs, sfbd)
			commitSiafundBDiff(tx, sfbd, modules.DiffApply)
		}
	}
}

// applyTransaction applies the contents of a transaction to the ConsensusSet.
// This produces a set of diffs, which are stored in the blockNode containing
// the transaction. No verification is done by this function.
func applyTransaction(tx *bolt.Tx, pb *processedBlockV2, t types.Transaction) {
	applySiacoinInputs(tx, pb, t)
	applySiacoinOutputs(tx, pb, t)
	applyFileContracts(tx, pb, t)
	applyFileContractRevisions(tx, pb, t)
	applyStorageProofs(tx, pb, t)
	applySiafundInputs(tx, pb, t)
	applySiafundOutputs(tx, pb, t)
}
