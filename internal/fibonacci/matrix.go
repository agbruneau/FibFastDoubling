package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

type MatrixExponentiation struct{}

func (me *MatrixExponentiation) Name() string {
	return "MatrixExponentiation (SymmetricOpt+Parallel+ZeroAlloc+LUT)"
}

// (squareSymmetricMatrix et multiplyMatrices sont inchangés par rapport à l'original, car optimaux)
func squareSymmetricMatrix(dest, m *matrix, s *matrixState, useParallel bool, threshold int) {
	var wg sync.WaitGroup

	t_a_sq := s.t1
	t_b_sq := s.t2
	t_d_sq := s.t3
	t_b_ad := s.t4
	t_a_plus_d := s.t5

	t_a_plus_d.Add(m.a, m.d)

	if useParallel && m.a.BitLen() > threshold {
		wg.Add(4)
		go func() { defer wg.Done(); t_a_sq.Mul(m.a, m.a) }()
		go func() { defer wg.Done(); t_b_sq.Mul(m.b, m.b) }()
		go func() { defer wg.Done(); t_d_sq.Mul(m.d, m.d) }()
		go func() { defer wg.Done(); t_b_ad.Mul(m.b, t_a_plus_d) }()
		wg.Wait()
	} else {
		t_a_sq.Mul(m.a, m.a)
		t_b_sq.Mul(m.b, m.b)
		t_d_sq.Mul(m.d, m.d)
		t_b_ad.Mul(m.b, t_a_plus_d)
	}

	dest.a.Add(t_a_sq, t_b_sq)
	dest.b.Set(t_b_ad)
	dest.c.Set(t_b_ad) // Symétrie
	dest.d.Add(t_b_sq, t_d_sq)
}

func multiplyMatrices(dest, m1, m2 *matrix, s *matrixState, useParallel bool, threshold int) {
	var wg sync.WaitGroup

	if useParallel && m1.a.BitLen() > threshold {
		wg.Add(8)
		go func() { defer wg.Done(); s.t1.Mul(m1.a, m2.a) }()
		go func() { defer wg.Done(); s.t2.Mul(m1.b, m2.c) }()
		go func() { defer wg.Done(); s.t3.Mul(m1.a, m2.b) }()
		go func() { defer wg.Done(); s.t4.Mul(m1.b, m2.d) }()
		go func() { defer wg.Done(); s.t5.Mul(m1.c, m2.a) }()
		go func() { defer wg.Done(); s.t6.Mul(m1.d, m2.c) }()
		go func() { defer wg.Done(); s.t7.Mul(m1.c, m2.b) }()
		go func() { defer wg.Done(); s.t8.Mul(m1.d, m2.d) }()
		wg.Wait()
	} else {
		s.t1.Mul(m1.a, m2.a)
		s.t2.Mul(m1.b, m2.c)
		s.t3.Mul(m1.a, m2.b)
		s.t4.Mul(m1.b, m2.d)
		s.t5.Mul(m1.c, m2.a)
		s.t6.Mul(m1.d, m2.c)
		s.t7.Mul(m1.c, m2.b)
		s.t8.Mul(m1.d, m2.d)
	}
	dest.a.Add(s.t1, s.t2)
	dest.b.Add(s.t3, s.t4)
	dest.c.Add(s.t5, s.t6)
	dest.d.Add(s.t7, s.t8)
}

func (me *MatrixExponentiation) CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	if n == 0 {
		return big.NewInt(0), nil
	}

	s := getMatrixState()
	defer putMatrixState(s)

	k := n - 1
	numBits := bits.Len64(k)
	invNumBits := 1.0
	if numBits > 0 {
		invNumBits = 1.0 / float64(numBits)
	}
	useParallel := runtime.NumCPU() > 1

	tempMatrix := s.tempMatrix

	// --- Algorithme d'Exponentiation par Carré (Bottom-Up) ---
	for i := 0; i < numBits; i++ {
		// Gestion de l'annulation.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if progressChan != nil {
			reportProgress(progressChan, calcIndex, float64(i)*invNumBits)
		}

		// ÉTAPE 1: Multiplication conditionnelle (Si le bit est 1).
		if (k>>uint(i))&1 == 1 {
			multiplyMatrices(tempMatrix, s.res, s.p, s, useParallel, threshold)
			s.res.Set(tempMatrix)
		}

		// ÉTAPE 2: Mise au carré pour l'itération suivante.
		// OPTIMISATION : Éviter le dernier carré inutile.
		if i < numBits-1 {
			squareSymmetricMatrix(tempMatrix, s.p, s, useParallel, threshold)
			s.p.Set(tempMatrix)
		}
	}

	reportProgress(progressChan, calcIndex, 1.0)
	return new(big.Int).Set(s.res.a), nil
}
