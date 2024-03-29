package merkletree

import (
	"errors"
)

// A CachedTree can be used to build Merkle roots and proofs from the cached
// Merkle roots of smaller blocks of data. Each CachedTree has a height,
// meaning every element added to the CachedTree is the root of a full Merkle
// tree containing 2^height leaves.
type CachedTree struct {
	CachedNodeHeight uint64 `json:"cached_node_index"`
	TrueProofIndex   uint64 `json:"true_proof_index"`
	Tree
}

// NewCachedTree initializes a CachedTree with the specified node height.
func NewCachedTree(cachedNodeHeight uint64) *CachedTree {
	return &CachedTree{
		CachedNodeHeight: cachedNodeHeight,
		Tree: Tree{
			CachedTree: true,
		},
	}
}

// Prove will create a proof that the leaf at the indicated index is a part of
// the data represented by the Merkle root of the Cached Tree. The CachedTree
// needs the proof set proving that the index is an element of the cached
// element in order to create a correct proof. After proof is called, the
// CachedTree is unchanged, and can receive more elements.
func (ct *CachedTree) Prove(cachedProofSet [][32]byte) (merkleRoot [32]byte, proofSet [][32]byte, proofIndex uint64, numLeaves uint64) {
	// Determine the proof index within the full tree, and the number of leaves
	// within the full tree.
	leavesPerCachedNode := uint64(1) << ct.CachedNodeHeight
	numLeaves = leavesPerCachedNode * ct.CurrentIndex

	// Get the proof set tail, which is generated based entirely on cached
	// nodes.
	merkleRoot, _, proofSetTail, _, _ := ct.Tree.Prove()
	if len(proofSetTail) < 1 {
		// The proof was invalid, return 'nil' for the proof set but accurate
		// values for everything else.
		return merkleRoot, nil, ct.TrueProofIndex, numLeaves
	}

	// The full proof set is going to be the input cachedProofSet combined with
	// the tail proof set. The one caveat is that the tail proof set has an
	// extra piece of data at the first element - the verifier will assume that
	// this data exists and therefore it needs to be omitted from the proof
	// set.
	proofSet = append(cachedProofSet, proofSetTail[1:]...)
	return merkleRoot, proofSet, ct.TrueProofIndex, numLeaves
}

// SetIndex will inform the CachedTree of the index of the leaf for which a
// storage proof is being created. The index should be the index of the actual
// leaf, and not the index of the cached element containing the leaf. SetIndex
// must be called on empty CachedTree.
func (ct *CachedTree) SetIndex(i uint64) error {
	if len(ct.Stack) != 0 {
		return errors.New("cannot call SetIndex on Tree if Tree has not been reset")
	}
	ct.TrueProofIndex = i
	return ct.Tree.SetIndex(i / (1 << ct.CachedNodeHeight))
}

// Clone copies existed cached tree to the new one.
func (ct *CachedTree) Clone() *CachedTree {
	tree := ct.Tree.Clone()
	return &CachedTree{
		Tree:             *tree,
		CachedNodeHeight: ct.CachedNodeHeight,
		TrueProofIndex:   ct.TrueProofIndex,
	}
}
