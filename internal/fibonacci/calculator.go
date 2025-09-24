package fibonacci

import (
	"context"
	"math/big"
	"sync"
)

const (
	MaxFibUint64             = 93
	DefaultParallelThreshold = 2048
)

// ProgressUpdate structure utilisée pour la communication concurrente de la progression.
type ProgressUpdate struct {
	CalculatorIndex int
	Value           float64
}

// Calculator définit l'interface standard.
// Modifié pour supporter l'orchestration simplifiée (ajout de calcIndex).
type Calculator interface {
	Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

type coreCalculator interface {
	CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// FibCalculator implémente le Design Pattern Décorateur (Fast Path O(1)).
type FibCalculator struct {
	core coreCalculator
}

func NewCalculator(core coreCalculator) Calculator {
	return &FibCalculator{core: core}
}

func (c *FibCalculator) Name() string {
	return c.core.Name()
}

func (c *FibCalculator) Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	// OPTIMISATION : Fast Path O(1)
	if n <= MaxFibUint64 {
		reportProgress(progressChan, calcIndex, 1.0)
		return lookupSmall(n), nil
	}

	// Cas complexe : Délégation au calculateur de cœur O(log n).
	return c.core.CalculateCore(ctx, progressChan, calcIndex, n, threshold)
}

// reportProgress effectue un envoi non bloquant sur le canal de progression.
func reportProgress(progressChan chan<- ProgressUpdate, calcIndex int, progress float64) {
	if progressChan == nil {
		return
	}
	update := ProgressUpdate{CalculatorIndex: calcIndex, Value: progress}
	// Communication Non Bloquante (Critique pour éviter la contention).
	select {
	case progressChan <- update:
	default: // Canal plein ou non prêt. On ignore la mise à jour.
	}
}

// --- LUT (Lookup Table) ---

var fibLookupTable [MaxFibUint64 + 1]*big.Int

func init() {
	var a, b uint64 = 0, 1
	for i := uint64(0); i <= MaxFibUint64; i++ {
		fibLookupTable[i] = new(big.Int).SetUint64(a)
		a, b = b, a+b
	}
}

func lookupSmall(n uint64) *big.Int {
	// Retourne une COPIE pour garantir l'immuabilité.
	return new(big.Int).Set(fibLookupTable[n])
}

// --- Pooling (Zéro-Allocation) ---

// Structures et Pools pour Fast Doubling
type calculationState struct {
	f_k, f_k1      *big.Int
	t1, t2, t3, t4 *big.Int
}

var statePool = sync.Pool{
	New: func() interface{} {
		return &calculationState{
			f_k: new(big.Int), f_k1: new(big.Int),
			t1: new(big.Int), t2: new(big.Int),
			t3: new(big.Int), t4: new(big.Int),
		}
	},
}

func getState() *calculationState {
	s := statePool.Get().(*calculationState)
	s.f_k.SetInt64(0)
	s.f_k1.SetInt64(1)
	return s
}

func putState(s *calculationState) {
	statePool.Put(s)
}

// Structures et Pools pour Matrix Exponentiation
type matrix struct {
	a, b, c, d *big.Int
}

func newMatrix() *matrix {
	return &matrix{new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
}

func (m *matrix) Set(other *matrix) {
	m.a.Set(other.a)
	m.b.Set(other.b)
	m.c.Set(other.c)
	m.d.Set(other.d)
}

type matrixState struct {
	res                            *matrix
	p                              *matrix
	tempMatrix                     *matrix
	t1, t2, t3, t4, t5, t6, t7, t8 *big.Int
}

var matrixStatePool = sync.Pool{
	New: func() interface{} {
		return &matrixState{
			res:        newMatrix(),
			p:          newMatrix(),
			tempMatrix: newMatrix(),
			t1:         new(big.Int), t2: new(big.Int), t3: new(big.Int), t4: new(big.Int),
			t5: new(big.Int), t6: new(big.Int), t7: new(big.Int), t8: new(big.Int),
		}
	},
}

func getMatrixState() *matrixState {
	s := matrixStatePool.Get().(*matrixState)
	// Init res (Identity Matrix)
	s.res.a.SetInt64(1)
	s.res.b.SetInt64(0)
	s.res.c.SetInt64(0)
	s.res.d.SetInt64(1)
	// Init p (Q Matrix)
	s.p.a.SetInt64(1)
	s.p.b.SetInt64(1)
	s.p.c.SetInt64(1)
	s.p.d.SetInt64(0)
	return s
}

func putMatrixState(s *matrixState) {
	matrixStatePool.Put(s)
}
