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

var errCorruptIndex = errors.New("corrupt spawn index")

// SpawnIndex is an encoded spawn index. It is read in place, as decoding
// every index of an invocation set takes 16 bytes per pair.
type SpawnIndex []byte

// EncodeSpawnIndex packs unique pairs as uvarint key deltas and versions.
func EncodeSpawnIndex(pairs []Pair) SpawnIndex {
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

// ForEach calls f with the pairs in key order until it returns false.
func (b SpawnIndex) ForEach(f func(Pair) bool) error {
	var key int64
	for len(b) > 0 {
		delta, n := binary.Uvarint(b)
		if n <= 0 {
			return errCorruptIndex
		}
		b = b[n:]
		version, n := binary.Uvarint(b)
		if n <= 0 {
			return errCorruptIndex
		}
		b = b[n:]
		key += int64(delta)
		if !f(Pair{KeyID: key, VersionID: int64(version)}) {
			return nil
		}
	}
	return nil
}

// Version returns the lowest version of a key, reading up to it.
func (b SpawnIndex) Version(keyID int64) (int64, bool, error) {
	var version int64
	found := false
	err := b.ForEach(func(p Pair) bool {
		if p.KeyID == keyID {
			version, found = p.VersionID, true
		}
		return p.KeyID < keyID
	})
	return version, found, err
}

// DecodeSpawnIndex unpacks an index written by EncodeSpawnIndex.
func DecodeSpawnIndex(b []byte) ([]Pair, error) {
	var out []Pair
	err := SpawnIndex(b).ForEach(func(p Pair) bool {
		out = append(out, p)
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
