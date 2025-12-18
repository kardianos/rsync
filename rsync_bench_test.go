package rsync

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func benchmarkRsync(b *testing.B, size int) {
	srcData := make([]byte, size)
	dstData := make([]byte, size)

	cc := &rand.ChaCha8{}
	cc.Read(srcData)
	copy(dstData, srcData)
	// Change some bytes to force delta.
	for range 100 {
		dstData[cc.Uint64()%uint64(size)] = byte(cc.Uint64())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs := &RSync{}

		targetReader := bytes.NewReader(dstData)
		var sig []BlockHash
		rs.CreateSignature(targetReader, func(bl BlockHash) error {
			sig = append(sig, bl)
			return nil
		})

		sourceReader := bytes.NewReader(srcData)
		ops := make(chan Operation, 100)
		go func() {
			defer close(ops)
			rs.CreateDelta(sourceReader, sig, func(op Operation) error {
				ops <- op
				return nil
			}, nil)
		}()

		// Drain ops.
		for range ops {
		}
	}
}

func BenchmarkRsync2M(b *testing.B) {
	const M = 1024 * 1024
	list := []struct {
		name string
		size int
	}{
		{"2M", 2 * M},
		{"10M", 10 * M},
		{"100M", 100 * M},
	}
	for _, item := range list {
		b.Run(item.name, func(b *testing.B) {
			benchmarkRsync(b, item.size)
		})
	}
}
