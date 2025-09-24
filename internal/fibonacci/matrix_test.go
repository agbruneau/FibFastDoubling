// EXPLICATION ACADÉMIQUE :
// Ce fichier de test valide l'implémentation de l'algorithme d'exponentiation
// matricielle. Il est crucial de tester non seulement la fonction principale
// `CalculateCore`, mais aussi les fonctions d'assistance internes comme
// `multiplyMatrices` et `squareSymmetricMatrix`, car la logique complexe s'y trouve.

package fibonacci

import (
	"context"
	"fmt"
	"math/big"
	"testing"
)

// bigInt convenience constructor
func bi(s string) *big.Int {
	i, _ := new(big.Int).SetString(s, 10)
	return i
}

// === Test 1: Test des fonctions d'assistance internes ===

// matrixToString est une fonction d'aide pour afficher les matrices dans les messages d'erreur.
func matrixToString(m *matrix) string {
	return fmt.Sprintf("[[a:%v, b:%v], [c:%v, d:%v]]", m.a, m.b, m.c, m.d)
}

// matrixEqual compare deux matrices pour l'égalité.
func matrixEqual(t *testing.T, got, want *matrix) bool {
	t.Helper()
	if got.a.Cmp(want.a) != 0 || got.b.Cmp(want.b) != 0 || got.c.Cmp(want.c) != 0 || got.d.Cmp(want.d) != 0 {
		t.Errorf("Matrix mismatch:\ngot:  %s\nwant: %s", matrixToString(got), matrixToString(want))
		return false
	}
	return true
}

// Teste la multiplication de deux matrices générales.
func TestMultiplyMatrices(t *testing.T) {
	// M1 = [[1, 2], [3, 4]]
	m1 := &matrix{a: bi("1"), b: bi("2"), c: bi("3"), d: bi("4")}
	// M2 = [[5, 6], [7, 8]]
	m2 := &matrix{a: bi("5"), b: bi("6"), c: bi("7"), d: bi("8")}
	// M1 * M2 = [[1*5+2*7, 1*6+2*8], [3*5+4*7, 3*6+4*8]] = [[19, 22], [43, 50]]
	want := &matrix{a: bi("19"), b: bi("22"), c: bi("43"), d: bi("50")}

	s := getMatrixState()
	defer putMatrixState(s)
	got := newMatrix()

	// On teste les deux chemins de code : séquentiel et parallèle.
	t.Run("Sequential", func(t *testing.T) {
		multiplyMatrices(got, m1, m2, s, false, 0)
		matrixEqual(t, got, want)
	})

	t.Run("Parallel", func(t *testing.T) {
		// Le seuil de 0 active toujours le parallélisme (si NumCPU > 1)
		multiplyMatrices(got, m1, m2, s, true, 0)
		matrixEqual(t, got, want)
	})
}

// Teste la mise au carré optimisée pour les matrices symétriques.
func TestSquareSymmetricMatrix(t *testing.T) {
	// M = [[3, 5], [5, 8]] (matrice symétrique)
	m := &matrix{a: bi("3"), b: bi("5"), c: bi("5"), d: bi("8")}
	// M^2 = [[3*3+5*5, 3*5+5*8], [5*3+8*5, 5*5+8*8]] = [[34, 55], [55, 89]]
	want := &matrix{a: bi("34"), b: bi("55"), c: bi("55"), d: bi("89")}

	s := getMatrixState()
	defer putMatrixState(s)
	got := newMatrix()

	t.Run("Sequential", func(t *testing.T) {
		squareSymmetricMatrix(got, m, s, false, 0)
		matrixEqual(t, got, want)
	})

	t.Run("Parallel", func(t *testing.T) {
		squareSymmetricMatrix(got, m, s, true, 0)
		matrixEqual(t, got, want)
	})
}


// === Test 2: Test d'exactitude de la fonction publique `CalculateCore` ===
func TestMatrixExponentiation_Correctness(t *testing.T) {
	// On réutilise les mêmes cas de test que pour FastDoubling pour garantir la cohérence.
	f100 := bi("354224848179261915075")
	f200 := bi("280571172992510140037611932413038677189525")

	testCases := []struct {
		n    uint64
		want *big.Int
		name string
	}{
		{0, big.NewInt(0), "F(0)"}, // Cas spécial géré au début de la fonction.
		{1, big.NewInt(1), "F(1)"},
		{2, big.NewInt(1), "F(2)"},
		{10, big.NewInt(55), "F(10)"},
		{93, fibLookupTable[93], "F(93)"},
		{94, new(big.Int).Add(fibLookupTable[93], fibLookupTable[92]), "F(94)"},
		{100, f100, "F(100)"},
		{200, f200, "F(200)"},
	}

	calc := &MatrixExponentiation{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calc.CalculateCore(context.Background(), nil, 0, tc.n, 0)
			if err != nil {
				t.Fatalf("CalculateCore returned an unexpected error: %v", err)
			}
			if got.Cmp(tc.want) != 0 {
				t.Errorf("CalculateCore(%d) got %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

// === Test 3: Test des fonctionnalités avancées (contexte, progression, etc.) ===
// Ces tests sont très similaires à ceux de FastDoubling, car l'interface
// et le comportement attendu (annulation, progression) sont les mêmes.

func TestMatrixExponentiation_ContextCancellation(t *testing.T) {
	calc := &MatrixExponentiation{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := calc.CalculateCore(ctx, nil, 0, 1_000_000, 0)

	if err != context.Canceled {
		t.Errorf("CalculateCore returned error %v, want %v", err, context.Canceled)
	}
}

func TestMatrixExponentiation_ProgressReporting(t *testing.T) {
	calc := &MatrixExponentiation{}
	progressChan := make(chan ProgressUpdate, 20)
	n := uint64(5000)

	_, err := calc.CalculateCore(context.Background(), progressChan, 1, n, 0)
	if err != nil {
		t.Fatalf("CalculateCore failed: %v", err)
	}
	close(progressChan)

	var updates []ProgressUpdate
	for update := range progressChan {
		updates = append(updates, update)
	}

	if len(updates) == 0 {
		t.Fatal("No progress updates were received")
	}

	if updates[len(updates)-1].Value != 1.0 {
		t.Errorf("Final progress was %f, want 1.0", updates[len(updates)-1].Value)
	}
}

func TestMatrixExponentiation_ParallelVsSequential(t *testing.T) {
	calc := &MatrixExponentiation{}
	n := uint64(50000)

	resultSeq, errSeq := calc.CalculateCore(context.Background(), nil, 0, n, 1_000_000) // Seuil élevé
	if errSeq != nil {
		t.Fatalf("Sequential execution failed: %v", errSeq)
	}

	resultPar, errPar := calc.CalculateCore(context.Background(), nil, 0, n, 128) // Seuil bas
	if errPar != nil {
		t.Fatalf("Parallel execution failed: %v", errPar)
	}

	if resultSeq.Cmp(resultPar) != 0 {
		t.Errorf("Sequential and parallel results do not match!")
	}
}

func TestMatrixExponentiation_ResultIsCopy(t *testing.T) {
	calc := &MatrixExponentiation{}
	n := uint64(150)

	result1, err1 := calc.CalculateCore(context.Background(), nil, 0, n, 0)
	if err1 != nil {
		t.Fatalf("First call failed: %v", err1)
	}
	expectedResult := new(big.Int).Set(result1)

	_, err2 := calc.CalculateCore(context.Background(), nil, 0, n+1, 0)
	if err2 != nil {
		t.Fatalf("Second call failed: %v", err2)
	}

	if result1.Cmp(expectedResult) != 0 {
		t.Errorf("Result from the first call was modified by the second call")
	}
}

func TestMatrixExponentiation_Name(t *testing.T) {
	calc := &MatrixExponentiation{}
	expectedName := "MatrixExponentiation (SymmetricOpt+Parallel+ZeroAlloc+LUT)"
	if name := calc.Name(); name != expectedName {
		t.Errorf("Name() = %q, want %q", name, expectedName)
	}
}
