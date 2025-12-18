package proto

import (
	"bytes"
	"testing"

	"github.com/kardianos/rsync"
)

func TestHeader(t *testing.T) {
	buf := &bytes.Buffer{}
	w := &Writer{
		Writer: buf,
	}

	expectBlockSize := 1024
	expectComp := CompNone
	expectType := TypeSignature
	err := w.Header(expectType, expectComp, expectBlockSize)
	if err != nil {
		t.Fatal(err)
	}

	r := &Reader{
		Reader: buf,
	}
	blockSize, err := r.Header(expectType)
	if err != nil {
		t.Fatal(err)
	}
	if blockSize != expectBlockSize {
		t.Errorf("Expected block size %d, got %d", expectBlockSize, blockSize)
	}
}

func TestSignature(t *testing.T) {
	buf := &bytes.Buffer{}
	w := &Writer{
		Writer: buf,
	}

	err := w.Header(TypeSignature, CompNone, 1024)
	if err != nil {
		t.Fatal(err)
	}

	input := []rsync.BlockHash{
		{Index: 0, WeakHash: 1234, StrongHash: []byte("strong1")},
		{Index: 1, WeakHash: 5678, StrongHash: []byte("strong2")},
	}

	sigWriter := w.SignatureWriter()
	for _, block := range input {
		err = sigWriter(block)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	r := &Reader{
		Reader: buf,
	}
	_, err = r.Header(TypeSignature)
	if err != nil {
		t.Fatal(err)
	}
	list, err := r.ReadAllSignatures()
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != len(input) {
		t.Fatalf("Expected %d signatures, got %d", len(input), len(list))
	}
	for i, block := range list {
		if block.Index != input[i].Index {
			t.Errorf("Index %d: expected index %d, got %d", i, input[i].Index, block.Index)
		}
		if block.WeakHash != input[i].WeakHash {
			t.Errorf("Index %d: expected weak hash %d, got %d", i, input[i].WeakHash, block.WeakHash)
		}
		if !bytes.Equal(block.StrongHash, input[i].StrongHash) {
			t.Errorf("Index %d: expected strong hash %s, got %s", i, input[i].StrongHash, block.StrongHash)
		}
	}
}

func TestDelta(t *testing.T) {
	buf := &bytes.Buffer{}
	w := &Writer{
		Writer: buf,
	}

	err := w.Header(TypeDelta, CompNone, 1024)
	if err != nil {
		t.Fatal(err)
	}

	input := []rsync.Operation{
		{Type: rsync.OpBlock, BlockIndex: 1},
		{Type: rsync.OpData, Data: []byte("hello")},
		{Type: rsync.OpBlockRange, BlockIndex: 5, BlockIndexEnd: 10},
		{Type: rsync.OpHash, Data: []byte("hash")},
	}

	opWriter := w.OperationWriter()
	for _, op := range input {
		err = opWriter(op)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	r := &Reader{
		Reader: buf,
	}
	_, err = r.Header(TypeDelta)
	if err != nil {
		t.Fatal(err)
	}

	ops := make(chan rsync.Operation, 100)
	hashOps := make(chan rsync.Operation, 100)
	go func() {
		defer close(ops)
		defer close(hashOps)
		err := r.ReadOperations(ops, hashOps)
		if err != nil {
			t.Error(err)
		}
	}()

	var output []rsync.Operation
	for op := range ops {
		output = append(output, op)
	}

	var outputHash []rsync.Operation
	for op := range hashOps {
		outputHash = append(outputHash, op)
	}

	expectedOps := []rsync.Operation{
		{Type: rsync.OpBlock, BlockIndex: 1},
		{Type: rsync.OpData, Data: []byte("hello")},
		{Type: rsync.OpBlockRange, BlockIndex: 5, BlockIndexEnd: 10},
	}
	expectedHashOps := []rsync.Operation{
		{Type: rsync.OpHash, Data: []byte("hash")},
	}

	if len(output) != len(expectedOps) {
		t.Fatalf("Expected %d ops, got %d", len(expectedOps), len(output))
	}
	for i, op := range output {
		if op.Type != expectedOps[i].Type {
			t.Errorf("Index %d: expected type %d, got %d", i, expectedOps[i].Type, op.Type)
		}
		if op.BlockIndex != expectedOps[i].BlockIndex {
			t.Errorf("Index %d: expected block index %d, got %d", i, expectedOps[i].BlockIndex, op.BlockIndex)
		}
		if op.BlockIndexEnd != expectedOps[i].BlockIndexEnd {
			t.Errorf("Index %d: expected block index end %d, got %d", i, expectedOps[i].BlockIndexEnd, op.BlockIndexEnd)
		}
		if !bytes.Equal(op.Data, expectedOps[i].Data) {
			t.Errorf("Index %d: expected data %s, got %s", i, expectedOps[i].Data, op.Data)
		}
	}

	if len(outputHash) != len(expectedHashOps) {
		t.Fatalf("Expected %d hash ops, got %d", len(expectedHashOps), len(outputHash))
	}
	for i, op := range outputHash {
		if op.Type != expectedHashOps[i].Type {
			t.Errorf("Hash Index %d: expected type %d, got %d", i, expectedHashOps[i].Type, op.Type)
		}
		if !bytes.Equal(op.Data, expectedHashOps[i].Data) {
			t.Errorf("Hash Index %d: expected data %s, got %s", i, expectedHashOps[i].Data, op.Data)
		}
	}
}

func TestErrors(t *testing.T) {
	// Test bad magic
	buf := &bytes.Buffer{}
	// Write bad magic
	buf.Write([]byte{0, 0, 0, 0})

	r := &Reader{
		Reader: buf,
	}
	_, err := r.Header(TypeSignature)
	if err != ErrBadMagic {
		t.Errorf("Expected ErrBadMagic, got %v", err)
	}

	// Test incorrect type
	buf.Reset()
	w := &Writer{Writer: buf}
	w.Header(TypeDelta, CompNone, 1024)

	r = &Reader{Reader: buf}
	_, err = r.Header(TypeSignature)
	if _, ok := err.(ErrIncorrectType); !ok {
		t.Errorf("Expected ErrIncorrectType, got %v", err)
	}
}
