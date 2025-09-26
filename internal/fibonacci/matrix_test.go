package fibonacci

import (
	"context"
	"math/big"
	"testing"
)

// simpleFib is a straightforward, iterative Fibonacci implementation for generating test data.
// It is correct by definition but slow, making it perfect for verifying the optimized calculators.
func simpleFib(n uint64) *big.Int {
	if n <= 1 {
		return big.NewInt(int64(n))
	}
	a, b := big.NewInt(0), big.NewInt(1)
	for i := uint64(1); i < n; i++ {
		a.Add(a, b)
		a, b = b, a
	}
	return b
}

// TestMatrixExponentiation_CalculateCore validates the matrix exponentiation calculator
// against the simple iterative Fibonacci implementation.
func TestMatrixExponentiation_CalculateCore(t *testing.T) {
	// Use the default threshold for parallel execution.
	const threshold = DefaultParallelThreshold

	// Instantiate the calculator to be tested.
	calc := &MatrixExponentiation{}

	testCases := []struct {
		name string
		n    uint64
	}{
		{"n=0", 0},
		{"n=1", 1},
		{"n=2", 2},
		{"n=10", 10},
		{"n=42", 42},
		{"MaxFibUint64 (n=93)", MaxFibUint64},
		{"First big.Int value (n=94)", MaxFibUint64 + 1},
		{"Large value (n=1000)", 1000},
		{"Very large value (n=5000)", 5000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Calculate the expected result using the simple, correct implementation.
			expected := simpleFib(tc.n)

			// Calculate the actual result using the high-performance calculator.
			// We use a background context and a nil progress reporter for this test.
			actual, err := calc.CalculateCore(context.Background(), nil, tc.n, threshold)

			if err != nil {
				t.Fatalf("CalculateCore returned an unexpected error: %v", err)
			}
			if actual.Cmp(expected) != 0 {
				t.Errorf("For n=%d, expected %s, but got %s", tc.n, expected.String(), actual.String())
			}
		})
	}
}
