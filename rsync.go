// RSync/RDiff implementation.
//
// Algorithm found at: http://www.samba.org/~tridge/phd_thesis.pdf
//
// Definitions
//   Source: The final content.
//   Target: The content to be made into final content.
//   Signature: The sequence of hashes used to identify the content.
package rsync

import (
	"bytes"
	"crypto/md5"
	"hash"
	"io"
)

// If no BlockSize is specified in the RSync instance, this value is used.
const DefaultBlockSize = 1024 * 6
const DefaultMaxDataOp = DefaultBlockSize * 10

// Internal constant used in rolling checksum.
const _M = 1 << 16

// Operation Types.
type OpType byte

const (
	OpBlock OpType = iota
	OpData
	OpHash
	OpBlockRange
)

// Instruction to mutate target to align to source.
type Operation struct {
	Type          OpType
	BlockIndex    uint64
	BlockIndexEnd uint64
	Data          []byte
}

// Signature hash item generated from target.
type BlockHash struct {
	Index      uint64
	StrongHash []byte
	WeakHash   uint32
}

// Write signatures as they are generated.
type SignatureWriter func(bl BlockHash) error
type OperationWriter func(op Operation) error

// Properties to use while working with the rsync algorithm.
// A single RSync should not be used concurrently as it may contain
// internal buffers and hash sums.
type RSync struct {
	BlockSize int
	MaxDataOp int

	// If this is nil an MD5 hash is used.
	UniqueHasher hash.Hash

	buffer []byte
}

// If the target length is known the number of hashes in the
// signature can be determined.
func (r *RSync) BlockHashCount(targetLength int) (count int) {
	if r.BlockSize <= 0 {
		r.BlockSize = DefaultBlockSize
	}
	count = (targetLength / r.BlockSize)
	if targetLength%r.BlockSize != 0 {
		count++
	}
	return
}

// Calculate the signature of target.
func (r *RSync) CreateSignature(target io.Reader, sw SignatureWriter) error {
	if r.BlockSize <= 0 {
		r.BlockSize = DefaultBlockSize
	}
	if r.UniqueHasher == nil {
		r.UniqueHasher = md5.New()
	}
	var err error
	var n int

	minBufferSize := r.BlockSize
	if len(r.buffer) < minBufferSize {
		r.buffer = make([]byte, minBufferSize)
	}
	buffer := r.buffer

	var block []byte
	loop := true
	var index uint64
	for loop {
		n, err = io.ReadAtLeast(target, buffer, r.BlockSize)
		if err != nil {
			// n == 0.
			if err == io.EOF {
				return nil
			}
			if err != io.ErrUnexpectedEOF {
				return err
			}
			// n > 0.
			loop = false
		}
		block = buffer[:n]
		weak, _, _ := βhash(block)
		err = sw(BlockHash{StrongHash: r.uniqueHash(block), WeakHash: weak, Index: index})
		if err != nil {
			return err
		}
		index++
	}
	return nil
}

// Apply the difference to the target. If alignedTargetSum is present the alignedTarget content will be written to it.
func (r *RSync) ApplyDelta(alignedTarget io.Writer, target io.ReadSeeker, ops chan Operation, alignedTargetSum hash.Hash) error {
	if r.BlockSize <= 0 {
		r.BlockSize = DefaultBlockSize
	}
	var err error
	var n int
	var block []byte

	minBufferSize := r.BlockSize
	if len(r.buffer) < minBufferSize {
		r.buffer = make([]byte, minBufferSize)
	}
	buffer := r.buffer

	writeBlock := func(op Operation) error {
		target.Seek(int64(r.BlockSize*int(op.BlockIndex)), 0)
		n, err = io.ReadAtLeast(target, buffer, r.BlockSize)
		if err != nil {
			if err != io.ErrUnexpectedEOF {
				return err
			}
		}
		block = buffer[:n]
		if alignedTargetSum != nil {
			alignedTargetSum.Write(block)
		}
		_, err = alignedTarget.Write(block)
		if err != nil {
			return err
		}
		return nil
	}

	for op := range ops {
		switch op.Type {
		case OpBlockRange:
			for i := op.BlockIndex; i <= op.BlockIndexEnd; i++ {
				err = writeBlock(Operation{
					Type:       OpBlock,
					BlockIndex: i,
				})
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
			}
		case OpBlock:
			err = writeBlock(op)
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
		case OpData:
			if alignedTargetSum != nil {
				alignedTargetSum.Write(op.Data)
			}
			_, err = alignedTarget.Write(op.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Create the operation list to mutate the target signature into the source.
// Any data operation from the OperationWriter must have the data copied out
// within the span of the function; the data buffer underlying the operation
// data is reused. The sourceSum create a complete hash sum of the source if
// present.
func (r *RSync) CreateDelta(source io.Reader, signature []BlockHash, ops OperationWriter, sourceSum hash.Hash) (err error) {
	if r.BlockSize <= 0 {
		r.BlockSize = DefaultBlockSize
	}
	if r.MaxDataOp <= 0 {
		r.MaxDataOp = DefaultMaxDataOp
	}
	if r.UniqueHasher == nil {
		r.UniqueHasher = md5.New()
	}
	minBufferSize := (r.BlockSize * 2) + (r.MaxDataOp)
	if len(r.buffer) < minBufferSize {
		r.buffer = make([]byte, minBufferSize)
	}
	buffer := r.buffer

	// A single β hashes may correlate with a many unique hashes.
	hashLookup := make(map[uint32][]BlockHash, len(signature))
	for _, h := range signature {
		key := h.WeakHash
		hashLookup[key] = append(hashLookup[key], h)
	}

	type section struct {
		tail int
		head int
	}

	var data, sum section
	var n, validTo int
	var αPop, αPush, β, β1, β2 uint32
	var blockIndex uint64
	var rolling, lastRun, foundHash bool

	var prevOp *Operation
	defer func() {
		if prevOp == nil {
			return
		}
		err = ops(*prevOp)
		prevOp = nil
	}()

	enqueue := func(op Operation) (err error) {
		switch op.Type {
		case OpBlock:
			if prevOp != nil {
				switch prevOp.Type {
				case OpBlock:
					if prevOp.BlockIndex+1 == op.BlockIndex {
						prevOp = &Operation{
							Type:          OpBlockRange,
							BlockIndex:    prevOp.BlockIndex,
							BlockIndexEnd: op.BlockIndex,
						}
						return
					}
				case OpBlockRange:
					if prevOp.BlockIndexEnd+1 == op.BlockIndex {
						prevOp.BlockIndexEnd = op.BlockIndex
						return
					}
				}
				err = ops(*prevOp)
				if err != nil {
					return
				}
				prevOp = nil
			}
			prevOp = &op
		case OpData:
			if prevOp != nil {
				err = ops(*prevOp)
				if err != nil {
					return
				}
			}
			err = ops(op)
			if err != nil {
				return
			}
			prevOp = nil
		}
		return
	}

	for !lastRun {
		// Determine if the buffer should be extended.
		if sum.tail+r.BlockSize > validTo {
			// Determine if the buffer should be wrapped.
			if validTo+r.BlockSize > len(buffer) {
				// Before wrapping the buffer, send any trailing data off.
				if data.tail < data.head {
					err = enqueue(Operation{Type: OpData, Data: buffer[data.tail:data.head]})
					if err != nil {
						return err
					}
				}
				// Wrap the buffer.
				l := validTo - sum.tail
				copy(buffer[:l], buffer[sum.tail:validTo])

				// Reset indexes.
				validTo = l
				sum.tail = 0
				data.head = 0
				data.tail = 0
			}

			n, err = io.ReadAtLeast(source, buffer[validTo:validTo+r.BlockSize], r.BlockSize)
			if sourceSum != nil {
				sourceSum.Write(buffer[validTo : validTo+n])
			}
			validTo += n
			if err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					return err
				}
				lastRun = true

				data.head = validTo
			}
			if n == 0 {
				break
			}
		}

		// Set the hash sum window head. Must either be a block size
		// or be at the end of the buffer.
		sum.head = min(sum.tail+r.BlockSize, validTo)

		// Compute the rolling hash.
		if !rolling {
			β, β1, β2 = βhash(buffer[sum.tail:sum.head])
			rolling = true
		} else {
			αPush = uint32(buffer[sum.head-1])
			β1 = (β1 - αPop + αPush) % _M
			β2 = (β2 - uint32(sum.head-sum.tail)*αPop + β1) % _M
			β = β1 + _M*β2
		}

		// Determine if there is a hash match.
		foundHash = false
		if hh, ok := hashLookup[β]; ok && !lastRun {
			blockIndex, foundHash = findUniqueHash(hh, r.uniqueHash(buffer[sum.tail:sum.head]))
		}
		// Send data off if there is data available and a hash is found (so the buffer before it
		// must be flushed first), or the data chunk size has reached it's maximum size (for buffer
		// allocation purposes) or to flush the end of the data.
		if data.tail < data.head && (foundHash || data.head-data.tail >= r.MaxDataOp || lastRun) {
			err = enqueue(Operation{Type: OpData, Data: buffer[data.tail:data.head]})
			if err != nil {
				return err
			}
			data.tail = data.head
		}

		if foundHash {
			err = enqueue(Operation{Type: OpBlock, BlockIndex: blockIndex})
			if err != nil {
				return err
			}
			rolling = false
			sum.tail += r.BlockSize

			// There is prior knowledge that any available data
			// buffered will have already been sent. Thus we can
			// assume data.head and data.tail are the same.
			// May trigger "data wrap".
			data.head = sum.tail
			data.tail = sum.tail
		} else {
			// The following is for the next loop iteration, so don't try to calculate if last.
			if !lastRun && rolling {
				αPop = uint32(buffer[sum.tail])
			}
			sum.tail += 1

			// May trigger "data wrap".
			data.head = sum.tail
		}
	}
	return nil
}

// Use a more unique way to identify a set of bytes.
func (r *RSync) uniqueHash(v []byte) []byte {
	r.UniqueHasher.Reset()
	r.UniqueHasher.Write(v)
	return r.UniqueHasher.Sum(nil)
}

// Searches for a given strong hash among all strong hashes in this bucket.
func findUniqueHash(hh []BlockHash, hashValue []byte) (uint64, bool) {
	if len(hashValue) == 0 {
		return 0, false
	}
	for _, block := range hh {
		if bytes.Equal(block.StrongHash, hashValue) {
			return block.Index, true
		}
	}
	return 0, false
}

// Use a faster way to identify a set of bytes.
func βhash(block []byte) (β uint32, β1 uint32, β2 uint32) {
	var a, b uint32
	for i, val := range block {
		a += uint32(val)
		b += (uint32(len(block)-1) - uint32(i) + 1) * uint32(val)
	}
	β = (a % _M) + (_M * (b % _M))
	β1 = a % _M
	β2 = b % _M
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
