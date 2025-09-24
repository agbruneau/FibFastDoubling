package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// OptimizedFastDoubling (Algorithme O(log n))
// Fondements Mathématiques :
// F(2k)   = F(k) * [2*F(k+1) - F(k)]
// F(2k+1) = F(k+1)² + F(k)²

type OptimizedFastDoubling struct{}

func (fd *OptimizedFastDoubling) Name() string {
	return "OptimizedFastDoubling (3-Way-Parallel+ZeroAlloc+LUT)"
}

func (fd *OptimizedFastDoubling) CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	s := getState()
	defer putState(s)

	numBits := bits.Len64(n)
	invNumBits := 1.0 / float64(numBits)

	var wg sync.WaitGroup
	useParallel := runtime.NumCPU() > 1

	// Boucle principale : Parcours des bits de n (MSB vers LSB).
	for i := numBits - 1; i >= 0; i-- {

		// --- Gestion de l'Annulation ---
		if ctx.Err() != nil {
			// Retourne l'erreur du contexte pour propagation via errgroup.
			return nil, ctx.Err()
		}
		if progressChan != nil && i < numBits-1 {
			reportProgress(progressChan, calcIndex, float64(numBits-1-i)*invNumBits)
		}

		// --- Étape 1: Doubling (Calcul de F(2k) et F(2k+1)) ---
		// Cette logique est optimale et préservée.

		// 1.1 Calcul du terme commun : (2*F(k+1) - F(k))
		s.t2.Lsh(s.f_k1, 1)
		s.t2.Sub(s.t2, s.f_k)

		// 1.2 Calcul des 3 multiplications indépendantes.
		if useParallel && s.f_k1.BitLen() > threshold {
			wg.Add(3)
			// Goroutine A: t3 = F(k) * t2
			go func(dest, src1, src2 *big.Int) {
				defer wg.Done()
				dest.Mul(src1, src2)
			}(s.t3, s.f_k, s.t2)

			// Goroutine B: t1 = F(k+1)²
			go func(dest, src *big.Int) {
				defer wg.Done()
				dest.Mul(src, src)
			}(s.t1, s.f_k1)

			// Goroutine C: t4 = F(k)²
			go func(dest, src *big.Int) {
				defer wg.Done()
				dest.Mul(src, src)
			}(s.t4, s.f_k)

			wg.Wait()

		} else {
			// Exécution séquentielle.
			s.t3.Mul(s.f_k, s.t2)
			s.t1.Mul(s.f_k1, s.f_k1)
			s.t4.Mul(s.f_k, s.f_k)
		}

		// Assemblage final.
		s.f_k.Set(s.t3)
		s.f_k1.Add(s.t1, s.t4)

		// --- Étape 2: Addition Conditionnelle ---
		if (n>>uint(i))&1 == 1 {
			// (F(k), F(k+1)) devient (F(k+1), F(k)+F(k+1)).
			// Utilisation de t1 pour l'échange (swap).
			s.t1.Set(s.f_k1)
			s.f_k1.Add(s.f_k1, s.f_k)
			s.f_k.Set(s.t1)
		}
	}

	reportProgress(progressChan, calcIndex, 1.0)
	// Retourne une copie car l'original sera retourné au pool.
	return new(big.Int).Set(s.f_k), nil
}
