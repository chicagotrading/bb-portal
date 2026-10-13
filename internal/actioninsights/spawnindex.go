// Package actioninsights computes action churn from spawn indexes.
package actioninsights

import (
	"encoding/binary"
	"errors"
	"sort"
)

// Pair is the action key and version of a spawn.
type Pair struct {
	KeyID     int64
	VersionID int64
}

// EncodeSpawnIndex packs unique pairs as uvarint key deltas and versions.
func EncodeSpawnIndex(pairs []Pair) []byte {
	sorted := append([]Pair(nil), pairs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].KeyID != sorted[j].KeyID {
			return sorted[i].KeyID < sorted[j].KeyID
		}
		return sorted[i].VersionID < sorted[j].VersionID
	})
	out := make([]byte, 0, 6*len(sorted))
	var prev Pair
	for i, p := range sorted {
		if i > 0 && p == prev {
			continue
		}
		out = binary.AppendUvarint(out, uint64(p.KeyID-prev.KeyID))
		out = binary.AppendUvarint(out, uint64(p.VersionID))
		prev = p
	}
	return out
}

// DecodeSpawnIndex unpacks an index written by EncodeSpawnIndex.
func DecodeSpawnIndex(b []byte) ([]Pair, error) {
	var out []Pair
	var key int64
	for len(b) > 0 {
		delta, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errors.New("corrupt spawn index")
		}
		b = b[n:]
		version, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errors.New("corrupt spawn index")
		}
		b = b[n:]
		key += int64(delta)
		out = append(out, Pair{KeyID: key, VersionID: int64(version)})
	}
	return out, nil
}
