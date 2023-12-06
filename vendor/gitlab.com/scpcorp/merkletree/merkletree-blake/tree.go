package merkletree

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/blake2b"
)

// A Tree takes data as leaves and returns the Merkle root. Each call to 'Push'
// adds one leaf to the Merkle tree. Calling 'Root' returns the Merkle root.
// The Tree also constructs proof that a single leaf is a part of the tree. The
// leaf can be chosen with 'SetIndex'. The memory footprint of Tree grows in
// O(log(n)) in the number of leaves.
type Tree struct {
	// The Tree is stored as a stack of subtrees. Each subtree has a height,
	// and is the Merkle root of 2^height leaves. A Tree with 11 nodes is
	// represented as a subtree of height 3 (8 nodes), a subtree of height 1 (2
	// nodes), and a subtree of height 0 (1 node). Head points to the smallest
	// tree. When a new leaf is inserted, it is inserted as a subtree of height
	// 0. If there is another subtree of the same height, both can be removed,
	// combined, and then inserted as a subtree of height n + 1.
	Stack []SubTree `json:"stack"`

	// Helper variables used to construct proofs that the data at 'proofIndex'
	// is in the Merkle tree. The proofSet is constructed as elements are being
	// added to the tree. The first element of the proof set is the original
	// data used to create the leaf at index 'proofIndex'. proofTree indicates
	// if the tree will be used to create a merkle proof.
	CurrentIndex uint64     `json:"current_index"`
	ProofIndex   uint64     `json:"proof_index"`
	ProofBase    []byte     `json:"proof_base"`
	ProofSet     [][32]byte `json:"proof_set"`
	ProofTree    bool       `json:"proof_tree"`

	// The CachedTree flag indicates that the tree is cached, meaning that
	// different code is used in 'Push' for creating a new head subtree. Adding
	// this flag is somewhat gross, but eliminates needing to duplicate the
	// entire 'Push' function when writing the cached tree.
	CachedTree bool `json:"cached_tree"`
}

// A SubTree contains the Merkle root of a complete (2^height leaves) SubTree
// of the Tree. 'sum' is the Merkle root of the SubTree.
type SubTree struct {
	Height int      `json:"height"` // a height over 300 is physically unachievable
	Sum    [32]byte `json:"sum"`
}

// LeafSum returns the hash created from data inserted to form a leaf. Leaf
// sums are calculated using:
//		Hash(0x00 || data)
func LeafSum(data []byte) [32]byte {
	buf := make([]byte, 0, 65)
	buf = append(buf, leafHashPrefix...)
	buf = append(buf, data...)
	return blake2b.Sum256(buf)
}

// nodeSum returns the hash created from two sibling nodes being combined into
// a parent node. Node sums are calculated using:
//		Hash(0x01 || left sibling sum || right sibling sum)
func nodeSum(a, b [32]byte) [32]byte {
	buf := make([]byte, 0, 65)
	buf = append(buf, nodeHashPrefix...)
	buf = append(buf, a[:]...)
	buf = append(buf, b[:]...)
	return blake2b.Sum256(buf)
}

// joinSubTrees combines two equal sized SubTrees into a larger SubTree.
func joinSubTrees(a, b SubTree) SubTree {
	if DEBUG {
		if a.Height < b.Height {
			panic("invalid subtree presented - height mismatch")
		}
	}

	return SubTree{
		Height: a.Height + 1,
		Sum:    nodeSum(a.Sum, b.Sum),
	}
}

// New creates a new Tree. BLAKE2b will be used for all hashing operations
// within the Tree.
func New() *Tree {
	return &Tree{
		// preallocate a stack large enough for most trees
		Stack: make([]SubTree, 0, 32),
	}
}

// Clone copies existed tree to the new one.
func (t *Tree) Clone() *Tree {
	newTree := &Tree{}
	*newTree = *t

	newTree.Stack = make([]SubTree, len(t.Stack))
	copy(newTree.Stack, t.Stack)
	newTree.ProofSet = make([][32]byte, len(t.ProofSet))
	copy(newTree.ProofSet, t.ProofSet)

	return newTree
}

// Prove creates a proof that the leaf at the established index (established by
// SetIndex) is an element of the Merkle tree. Prove will return a nil proof
// set if used incorrectly. Prove does not modify the Tree. Prove can only be
// called if SetIndex has been called previously.
func (t *Tree) Prove() (merkleRoot [32]byte, base []byte, proofSet [][32]byte, proofIndex uint64, numLeaves uint64) {
	if !t.ProofTree {
		panic("wrong usage: can't call prove on a tree if SetIndex wasn't called")
	}

	// Return nil if the Tree is empty, or if the proofIndex hasn't yet been
	// reached.
	if len(t.Stack) == 0 || len(t.ProofSet) == 0 {
		return t.Root(), nil, nil, t.ProofIndex, t.CurrentIndex
	}
	proofSet = t.ProofSet

	// The set of subtrees must now be collapsed into a single root. The proof
	// set already contains all of the elements that are members of a complete
	// subtree. Of what remains, there will be at most 1 element provided from
	// a sibling on the right, and all of the other proofs will be provided
	// from a sibling on the left. This results from the way orphans are
	// treated. All subtrees smaller than the subtree containing the proofIndex
	// will be combined into a single subtree that gets combined with the
	// proofIndex subtree as a single right sibling. All subtrees larger than
	// the subtree containing the proofIndex will be combined with the subtree
	// containing the proof index as left siblings.

	// Start at the smallest subtree and combine it with larger subtrees until
	// it would be combining with the subtree that contains the proof index. We
	// can recognize the subtree containing the proof index because the height
	// of that subtree will be one less than the current length of the proof
	// set.
	i := len(t.Stack) - 1
	current := t.Stack[i]
	for i--; i >= 0 && t.Stack[i].Height < len(proofSet)-1; i-- {
		current = joinSubTrees(t.Stack[i], current)
	}

	// Sanity check - check that either 'current' or 'current.next' is the
	// subtree containing the proof index.
	if DEBUG {
		if current.Height != len(t.ProofSet)-1 && (i >= 0 && t.Stack[i].Height != len(t.ProofSet)-1) {
			panic("could not find the subtree containing the proof index")
		}
	}

	// If the current subtree is not the subtree containing the proof index,
	// then it must be an aggregate subtree that is to the right of the subtree
	// containing the proof index, and the next subtree is the subtree
	// containing the proof index.
	if i >= 0 && t.Stack[i].Height == len(proofSet)-1 {
		proofSet = append(proofSet, current.Sum)
		current = t.Stack[i]
		i--
	}

	// The current subtree must be the subtree containing the proof index. This
	// subtree does not need an entry, as the entry was created during the
	// construction of the Tree. Instead, skip to the next subtree.
	//
	// All remaining subtrees will be added to the proof set as a left sibling,
	// completing the proof set.
	for ; i >= 0; i-- {
		current = t.Stack[i]
		proofSet = append(proofSet, current.Sum)
	}
	return t.Root(), t.ProofBase, proofSet, t.ProofIndex, t.CurrentIndex
}

// Push will add data to the set, building out the Merkle tree and Root. The
// tree does not remember all elements that are added, instead only keeping the
// log(n) elements that are necessary to build the Merkle root and keeping the
// log(n) elements necessary to build a proof that a piece of data is in the
// Merkle tree.
func (t *Tree) Push(data []byte) {
	if t.CachedTree {
		panic("cannot call Push on a cached tree")
	}
	// The first element of a proof is the data at the proof index. If this
	// data is being inserted at the proof index, it is added to the proof set.
	if t.CurrentIndex == t.ProofIndex {
		t.ProofBase = data
		t.ProofSet = append(t.ProofSet, LeafSum(data))
	}

	// Hash the data to create a subtree of height 0. The sum of the new node
	// is going to be the data for cached trees, and is going to be the result
	// of calling LeafSum() on the data for standard trees. Doing a check here
	// prevents needing to duplicate the entire 'Push' function for the trees.
	t.Stack = append(t.Stack, SubTree{
		Height: 0,
		Sum:    LeafSum(data),
	})

	// Join SubTrees if possible.
	t.joinAllSubTrees()

	// Update the index.
	t.CurrentIndex++
}

// PushSubTree pushes a cached subtree into the merkle tree. The subtree has to
// be smaller than the smallest subtree in the merkle tree, it has to be
// balanced and it can't contain the element that needs to be proven.  Since we
// can't tell if a SubTree is balanced, we can't sanity check for unbalanced
// trees. Therefore an unbalanced tree will cause silent errors, pain and
// misery for the person who wants to debug the resulting error.
func (t *Tree) PushSubTree(height int, sum [32]byte) error {
	newIndex := t.CurrentIndex + 1<<uint64(height)

	// If pushing a subtree of height 0 at the proof index, add the hash to the
	// proof set. Otherwise, the subtree containing the proof index should not
	// be pushed.
	if t.ProofTree {
		if t.CurrentIndex == t.ProofIndex && height == 0 {
			t.ProofSet = append(t.ProofSet, sum)
		} else if t.CurrentIndex <= t.ProofIndex && t.ProofIndex < newIndex {
			return errors.New("the cached tree shouldn't contain the element to prove")
		}
	}

	// We can only add the cached tree if its depth is <= the depth of the
	// current subtree.
	if len(t.Stack) != 0 && height > t.Stack[len(t.Stack)-1].Height {
		return fmt.Errorf("can't add a subtree that is larger than the smallest subtree %v > %v", height, t.Stack[len(t.Stack)-1].Height)
	}

	// Insert the cached tree as the new head.
	t.Stack = append(t.Stack, SubTree{
		Height: height,
		Sum:    sum,
	})

	// Join SubTrees if possible.
	t.joinAllSubTrees()

	// Update the index.
	t.CurrentIndex = newIndex

	return nil
}

// Root returns the Merkle root of the data that has been pushed.
func (t *Tree) Root() [32]byte {
	// If the Tree is empty, return nil.
	if len(t.Stack) == 0 {
		return [32]byte{}
	}

	// The root is formed by hashing together SubTrees in order from least in
	// height to greatest in height. The taller subtree is the first subtree in
	// the join.
	current := t.Stack[len(t.Stack)-1]
	for i := len(t.Stack) - 2; i >= 0; i-- {
		current = joinSubTrees(t.Stack[i], current)
	}
	return current.Sum
}

// SetIndex will tell the Tree to create a storage proof for the leaf at the
// input index. SetIndex must be called on an empty tree.
func (t *Tree) SetIndex(i uint64) error {
	if len(t.Stack) != 0 {
		return errors.New("cannot call SetIndex on Tree if Tree has not been reset")
	}
	t.ProofTree = true
	t.ProofIndex = i
	return nil
}

// joinAllSubTrees inserts the SubTree at t.head into the Tree. As long as the
// height of the next SubTree is the same as the height of the current SubTree,
// the two will be combined into a single SubTree of height n+1.
func (t *Tree) joinAllSubTrees() {
	for len(t.Stack) > 1 && t.Stack[len(t.Stack)-1].Height == t.Stack[len(t.Stack)-2].Height {
		i := len(t.Stack) - 1
		j := len(t.Stack) - 2

		// Before combining subtrees, check whether one of the subtree hashes
		// needs to be added to the proof set. This is going to be true IFF the
		// subtrees being combined are one height higher than the previous
		// subtree added to the proof set. The height of the previous subtree
		// added to the proof set is equal to len(t.ProofSet) - 1.
		if t.Stack[i].Height == len(t.ProofSet)-1 {
			// One of the subtrees needs to be added to the proof set. The
			// subtree that needs to be added is the subtree that does not
			// contain the proofIndex. Because the subtrees being compared are
			// the smallest and rightmost trees in the Tree, this can be
			// determined by rounding the currentIndex down to the number of
			// nodes in the subtree and comparing that index to the proofIndex.
			leaves := uint64(1 << uint(t.Stack[i].Height))
			mid := (t.CurrentIndex / leaves) * leaves
			if t.ProofIndex < mid {
				t.ProofSet = append(t.ProofSet, t.Stack[i].Sum)
			} else {
				t.ProofSet = append(t.ProofSet, t.Stack[j].Sum)
			}

			// Sanity check - the proofIndex should never be less than the
			// midpoint minus the number of leaves in each subtree.
			if DEBUG {
				if t.ProofIndex < mid-leaves {
					panic("proof being added with weird values")
				}
			}
		}

		// Join the two SubTrees into one SubTree with a greater height.
		t.Stack = append(t.Stack[:j], joinSubTrees(t.Stack[j], t.Stack[i]))
	}

	// Sanity check - From head to tail of the stack, the height should be
	// strictly decreasing.
	if DEBUG {
		for i := range t.Stack[1:] {
			if t.Stack[i].Height <= t.Stack[i+1].Height {
				panic("subtrees are out of order")
			}
		}
	}
}
