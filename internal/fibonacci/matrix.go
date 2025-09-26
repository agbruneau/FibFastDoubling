// ACADEMIC EXPLANATION:
// This file implements Fibonacci calculation via matrix exponentiation,
// another O(log n) method. It is often conceptually easier to understand
// than Fast Doubling but can be slightly less performant in practice
// due to a higher number of multiplications.
package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// MatrixExponentiation is an implementation of the `coreCalculator` interface.
// ACADEMIC EXPLANATION: The Matrix Exponentiation Algorithm (O(log n))
//
// This method relies on the fact that the Fibonacci sequence can be expressed
// by a linear transformation represented by a matrix.
//
//	[ F(n+1) ] = [ 1  1 ] * [ F(n)   ]
//	[ F(n)   ]   [ 1  0 ]   [ F(n-1) ]
//
// By applying this transformation `n` times, we get:
//
//	[ F(n+1) ] = [ 1  1 ]^n * [ F(1) ]
//	[ F(n)   ]   [ 1  0 ]    [ F(0) ]
//
// Since F(1)=1 and F(0)=0, calculating F(n) reduces to calculating the matrix
// Q = [[1, 1], [1, 0]] raised to the power `n-1`, and taking the top-left element.
// The calculation of Q^n can be done very efficiently in O(log n)
// steps using the "exponentiation by squaring" algorithm.
type MatrixExponentiation struct{}

func (c *MatrixExponentiation) Name() string {
	return "MatrixExponentiation (SymmetricOpt+Parallel+ZeroAlloc+LUT)"
}

// executeTasks runs a slice of functions, either sequentially or in parallel,
// based on the provided flag. This utility abstracts the WaitGroup logic.
func executeTasks(inParallel bool, tasks []func()) {
	if inParallel {
		var wg sync.WaitGroup
		wg.Add(len(tasks))
		for _, task := range tasks {
			go func(f func()) {
				defer wg.Done()
				f()
			}(task)
		}
		wg.Wait()
	} else {
		for _, task := range tasks {
			task()
		}
	}
}

// squareSymmetricMatrix calculates the square of a symmetric matrix.
// ACADEMIC EXPLANATION: Optimization for Symmetric Matrices
// A standard 2x2 matrix multiplication (M * M) requires 8 integer multiplications.
// However, if matrix M is symmetric (element [0,1] equals element [1,0]),
// we can optimize the calculation.
//
// Let M = [[a, b], [b, d]].
// M² = [[a²+b², ab+bd], [ab+bd, b²+d²]]
//
// We can calculate all terms of M² with only 4 expensive integer multiplications:
// a², b², d², and b*(a+d).
// This is a significant optimization that halves the number of `big.Int.Mul` calls.
func squareSymmetricMatrix(dest, mat *matrix, state *matrixState, useParallel bool, threshold int) {
	// Use temporary integers from the state pool.
	aSquared := state.t1
	bSquared := state.t2
	dSquared := state.t3
	bTimesAPlusD := state.t4
	aPlusD := state.t5

	aPlusD.Add(mat.a, mat.d)

	tasks := []func(){
		func() { aSquared.Mul(mat.a, mat.a) },
		func() { bSquared.Mul(mat.b, mat.b) },
		func() { dSquared.Mul(mat.d, mat.d) },
		func() { bTimesAPlusD.Mul(mat.b, aPlusD) },
	}

	// Execute multiplications, in parallel if numbers are large enough.
	shouldRunInParallel := useParallel && mat.a.BitLen() > threshold
	executeTasks(shouldRunInParallel, tasks)

	// Assemble the final result.
	dest.a.Add(aSquared, bSquared)
	dest.b.Set(bTimesAPlusD)
	dest.c.Set(bTimesAPlusD) // Symmetry is preserved.
	dest.d.Add(bSquared, dSquared)
}

// multiplyMatrices performs a standard 2x2 matrix multiplication.
func multiplyMatrices(dest, m1, m2 *matrix, state *matrixState, useParallel bool, threshold int) {
	tasks := []func(){
		func() { state.t1.Mul(m1.a, m2.a) },
		func() { state.t2.Mul(m1.b, m2.c) },
		func() { state.t3.Mul(m1.a, m2.b) },
		func() { state.t4.Mul(m1.b, m2.d) },
		func() { state.t5.Mul(m1.c, m2.a) },
		func() { state.t6.Mul(m1.d, m2.c) },
		func() { state.t7.Mul(m1.c, m2.b) },
		func() { state.t8.Mul(m1.d, m2.d) },
	}

	// A standard 2x2 matrix multiplication requires 8 independent integer multiplications.
	// We run them in parallel if the numbers are large enough.
	shouldRunInParallel := useParallel && m1.a.BitLen() > threshold
	executeTasks(shouldRunInParallel, tasks)

	// The final assembly (additions) is done sequentially.
	dest.a.Add(state.t1, state.t2)
	dest.b.Add(state.t3, state.t4)
	dest.c.Add(state.t5, state.t6)
	dest.d.Add(state.t7, state.t8)
}

// CalculateCore implements the exponentiation by squaring algorithm.
// This version uses pointer swapping instead of copying matrix data to avoid
// overhead, making the state transitions within the loop more efficient.
func (c *MatrixExponentiation) CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error) {
	if n == 0 {
		return big.NewInt(0), nil
	}

	// Acquire state (matrices and temporary integers) from the pool.
	state := acquireMatrixState()
	defer releaseMatrixState(state)

	// We need Q^(n-1) to find F(n).
	exponent := n - 1
	numBits := bits.Len64(exponent)

	// Prevent division by zero for n=1 (exponent=0, numBits=0).
	var invNumBits float64
	if numBits > 0 {
		invNumBits = 1.0 / float64(numBits)
	}

	useParallel := runtime.NumCPU() > 1

	// Aliases for the matrices from the pool.
	// resultMatrix holds the accumulated result, starts as Identity.
	// powerMatrix holds the current power of Q, starts as Q.
	// tempMatrix is used as a destination for calculations before swapping.
	resultMatrix := state.res
	powerMatrix := state.p
	tempMatrix := state.tempMatrix

	// --- BINARY EXPONENTIATION (EXPONENTIATION BY SQUARING) ALGORITHM ---
	// We iterate through the bits of the exponent from LSB to MSB.
	for i := 0; i < numBits; i++ {
		// Cooperative cancellation check.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if reporter != nil {
			reporter(float64(i) * invNumBits)
		}

		// STEP 1: CONDITIONAL MULTIPLICATION
		// If the i-th bit of the exponent is 1, multiply our current result
		// by the current power of the base matrix: result = result * power.
		if (exponent>>uint(i))&1 == 1 {
			// The result of the multiplication is placed in tempMatrix.
			multiplyMatrices(tempMatrix, resultMatrix, powerMatrix, state, useParallel, threshold)
			// Swap pointers: resultMatrix now points to the new result, and
			// the old result matrix becomes the new temporary buffer.
			resultMatrix, tempMatrix = tempMatrix, resultMatrix
		}

		// STEP 2: SQUARING
		// Square the power matrix for the next iteration: power = power * power.
		// Note: The power matrix `p` remains symmetric throughout the process,
		// so we can use the optimized squaring function.
		if i < numBits-1 { // Optimization: avoid the last, unnecessary squaring.
			// The result of the squaring is placed in tempMatrix.
			squareSymmetricMatrix(tempMatrix, powerMatrix, state, useParallel, threshold)
			// Swap pointers: powerMatrix now points to the new squared value, and
			// the old power matrix becomes the new temporary buffer.
			powerMatrix, tempMatrix = tempMatrix, powerMatrix
		}
	}

	// The final progress (1.0) is guaranteed by the decorator (FibCalculator).
	// The result F(n) is in the top-left element of the final result matrix.
	// Return a copy to ensure isolation from the pool.
	return new(big.Int).Set(resultMatrix.a), nil
}
