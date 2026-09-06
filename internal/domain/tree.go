package domain

import "fmt"

// AncestorScanLimit caps how far a cycle guard will walk.
//
// It must equal the LIMIT in the ancestor-chain queries -- CategoryAncestorIDs
// and LocationAncestorIDs -- and scripts/archlint.sh checks that it does. The
// number lived in four places before that: two Go constants, one per write
// path, and the two SQL literals they were supposed to match.
//
// Reaching it is an error rather than a truncation. The query is capped so that
// a cycle fails loudly instead of hanging, so a full result means the tree is
// already corrupt, which is not a thing to paper over by returning the first
// 256 answers.
const AncestorScanLimit = 256

// HasAncestor reports whether ancestor appears in a node's ancestor chain,
// which is what makes re-parenting into a cycle refusable.
//
// It takes the chain rather than fetching it, because Categories and Locations
// are stored by different write paths and read by different queries, and the
// part they share is this: the same walk, the same cap, the same conclusion.
// Both had their own copy of it, identical but for the identifier's type.
//
// The caller names the node it was asked about. This function knows the rule
// and not the vocabulary.
func HasAncestor[ID ~int64](chain []int64, ancestor ID) (bool, error) {
	if len(chain) >= AncestorScanLimit {
		return false, fmt.Errorf("ancestor chain exceeds %d nodes; tree is corrupt", AncestorScanLimit)
	}
	for _, id := range chain {
		if ID(id) == ancestor {
			return true, nil
		}
	}
	return false, nil
}
