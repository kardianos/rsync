package sbuffer

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"
)

func TestBuffer(t *testing.T) {
	type step struct {
		action   string // "next", "used"
		val      int
		expect   string // We'll compare as string for ease
		err      error
		panics   bool
		panicErr error // Optional specific panic error to check
	}

	tests := []struct {
		name       string
		bufferSize int
		input      string
		steps      []step
	}{
		{
			name:       "Basic Read",
			bufferSize: 10,
			input:      "hello world",
			steps: []step{
				{action: "next", val: 5, expect: "hello", err: nil},
				{action: "used", val: 5},
				{action: "next", val: 6, expect: " world", err: nil}, // " world" len is 6
			},
		},
		{
			name:       "Buffer Wrap (Copy)",
			bufferSize: 10,
			input:      "0123456789abcdef",
			steps: []step{
				{action: "next", val: 5, expect: "01234", err: nil},
				{action: "used", val: 5},
				// Buffer has [5:5] active (empty), tail at 5.
				// Requesting 6. max cap is 10. 5+6 = 11 > 10.
				// Should trigger copy.
				{action: "next", val: 5, expect: "56789", err: nil},
			},
		},
		{
			name:       "EOF Handling",
			bufferSize: 20,
			input:      "short",
			steps: []step{
				{action: "next", val: 10, expect: "short", err: io.EOF},
			},
		},
		{
			name:       "Requested too much capacity",
			bufferSize: 5,
			input:      "abcdef",
			steps: []step{
				{action: "next", val: 6, panics: true, panicErr: ErrNeedCap},
			},
		},
		{
			name:       "Used too much",
			bufferSize: 10,
			input:      "abc",
			steps: []step{
				{action: "next", val: 3, expect: "abc", err: nil},
				{action: "used", val: 4, panics: true, panicErr: ErrUsedTooMuch},
			},
		},
		{
			name:       "Partial Read Loop",
			bufferSize: 10,
			input:      "abcdef",
			steps: []step{
				// This specifically targets the loop in Next where it calls Read multiple times if needed.
				// However, strings.Reader usually returns all available.
				// We rely on the buffer filling up logic correctness here mainly.
				{action: "next", val: 3, expect: "abc", err: nil},
				{action: "used", val: 3},
				{action: "next", val: 3, expect: "def", err: nil},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewBufferString(tc.input)
			b := NewBuffer(r, tc.bufferSize)

			for i, s := range tc.steps {
				func() {
					defer func() {
						if r := recover(); r != nil {
							if !s.panics {
								t.Errorf("step %d: unexpected panic: %v", i, r)
							} else if s.panicErr != nil && r != s.panicErr {
								t.Errorf("step %d: expected panic %v, got %v", i, s.panicErr, r)
							}
						} else if s.panics {
							t.Errorf("step %d: expected panic, got none", i)
						}
					}()

					switch s.action {
					case "next":
						data, err := b.Next(s.val)
						if !errors.Is(err, s.err) {
							t.Errorf("step %d: expected error %v, got %v", i, s.err, err)
						}
						if string(data) != s.expect {
							t.Errorf("step %d: expected data %q, got %q", i, s.expect, string(data))
						}
					case "used":
						b.Used(s.val)
					}
				}()
			}
		})
	}
}

func TestRandomized(t *testing.T) {
	// Seed random
	seed := time.Now().UnixNano()
	t.Logf("Random seed: %d", seed)
	rng := rand.New(rand.NewSource(seed))

	// Data size 100KB
	dataSize := 1024 * 100
	inputData := make([]byte, dataSize)
	rng.Read(inputData)

	// Reader
	r := bytes.NewReader(inputData)

	// Small buffer to force frequent wrapping
	bufferSize := 100
	b := NewBuffer(r, bufferSize)

	var outputData []byte
	readTotal := 0

	for readTotal < dataSize {
		// Random needed amount [1, 50]
		needed := rng.Intn(50) + 1

		// Call Next
		chunk, err := b.Next(needed)
		if err != nil && err != io.EOF {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Consume it
		// Randomly consume part or all
		amountToUse := len(chunk)
		if amountToUse > 0 {
			// Bias towards using all to progress faster, but use partial enough
			if rng.Intn(3) == 0 {
				amountToUse = rng.Intn(len(chunk)) + 1
			}
		}

		// We only append what we actually consume
		outputData = append(outputData, chunk[:amountToUse]...)
		readTotal += amountToUse

		b.Used(amountToUse)

		if err == io.EOF && len(chunk) == 0 {
			// If we got EOF and no data, we are done.
			// Ideally Next returns whatever is left + EOF.
			break
		}
		// If we got EOF but data, we loop again to consume it (or just finish if we consumed all)
		// Logic: inputData is finite. readTotal counts what we consumed.
		// If we consumed part of the last chunk, next call gives remainder.
	}

	// Verify
	if !bytes.Equal(inputData, outputData) {
		t.Errorf("Data mismatch!")
		// Find first diff
		for i := 0; i < len(inputData) && i < len(outputData); i++ {
			if inputData[i] != outputData[i] {
				t.Fatalf("Mismatch at offset %d: expected %x, got %x", i, inputData[i], outputData[i])
			}
		}
		if len(inputData) != len(outputData) {
			t.Fatalf("Length mismatch: expected %d, got %d", len(inputData), len(outputData))
		}
	}
}
