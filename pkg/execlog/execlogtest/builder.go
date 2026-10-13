// Package execlogtest writes synthetic compact execution logs for tests.
package execlogtest

import (
	"bytes"

	spawnpb "github.com/bazelbuild/bazel/src/main/protobuf"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/encoding/protodelim"
)

// Builder appends entries with increasing IDs.
type Builder struct {
	entries []*spawnpb.ExecLogEntry
	nextID  uint32
}

// Add appends an entry and returns its ID.
func (b *Builder) Add(e *spawnpb.ExecLogEntry) uint32 {
	b.nextID++
	e.Id = b.nextID
	b.entries = append(b.entries, e)
	return e.Id
}

// Digest returns a digest whose size is the length of the hash.
func Digest(hash string) *spawnpb.Digest {
	return &spawnpb.Digest{Hash: hash, SizeBytes: int64(len(hash))}
}

// File adds a file entry.
func (b *Builder) File(path, hash string) uint32 {
	return b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_File_{File: &spawnpb.ExecLogEntry_File{Path: path, Digest: Digest(hash)}}})
}

// Dir adds a directory entry with files relative to it.
func (b *Builder) Dir(path string, files map[string]string) uint32 {
	d := &spawnpb.ExecLogEntry_Directory{Path: path}
	for p, h := range files {
		d.Files = append(d.Files, &spawnpb.ExecLogEntry_File{Path: p, Digest: Digest(h)})
	}
	return b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Directory_{Directory: d}})
}

// Set adds an input set.
func (b *Builder) Set(inputs []uint32, transitive ...uint32) uint32 {
	return b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_InputSet_{InputSet: &spawnpb.ExecLogEntry_InputSet{InputIds: inputs, TransitiveSetIds: transitive}}})
}

// Spawn adds a spawn with the given output entry IDs.
func (b *Builder) Spawn(s *spawnpb.ExecLogEntry_Spawn, outputs ...uint32) {
	for _, o := range outputs {
		s.Outputs = append(s.Outputs, &spawnpb.ExecLogEntry_Output{Type: &spawnpb.ExecLogEntry_Output_OutputId{OutputId: o}})
	}
	b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Spawn_{Spawn: s}})
}

// Bytes returns the uncompressed log.
func (b *Builder) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	for _, e := range b.entries {
		if _, err := protodelim.MarshalTo(&buf, e); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// Compress zstd compresses a log, as Bazel publishes it.
func Compress(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	enc, err := zstd.NewWriter(&out)
	if err != nil {
		return nil, err
	}
	if _, err := enc.Write(raw); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
