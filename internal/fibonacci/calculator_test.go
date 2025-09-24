// EXPLICATION ACADÉMIQUE :
// Ce fichier de test démontre plusieurs techniques de test avancées en Go.
// Chaque fonction de test est conçue pour être indépendante et pour valider
// une partie spécifique du code de `calculator.go`.

package fibonacci

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"
)

// === Test 1: Validation de l'initialisation (fonction init) ===
// L'objectif est de s'assurer que la table de consultation (lookup table)
// a été correctement remplie par la fonction `init()`.
func TestFibLookupTableInitialization(t *testing.T) {
	// Table de tests (test cases) : une approche structurée pour définir les entrées et les sorties attendues.
	testCases := []struct {
		n      uint64
		want   *big.Int
		caseName string
	}{
		{0, big.NewInt(0), "F(0)"},
		{1, big.NewInt(1), "F(1)"},
		{2, big.NewInt(1), "F(2)"},
		{10, big.NewInt(55), "F(10)"},
		{20, big.NewInt(6765), "F(20)"},
		{MaxFibUint64, new(big.Int).SetUint64(12200160415121876738), "F(93) - Max"},
	}

	for _, tc := range testCases {
		// t.Run permet de créer des sous-tests, ce qui améliore la lisibilité des résultats.
		t.Run(tc.caseName, func(t *testing.T) {
			if fibLookupTable[tc.n].Cmp(tc.want) != 0 {
				t.Errorf("fibLookupTable[%d] = %v, want %v", tc.n, fibLookupTable[tc.n], tc.want)
			}
		})
	}
}

// === Test 2: Validation de la copie immuable ===
// On teste `lookupSmall` pour s'assurer qu'elle retourne une copie des données
// et non un pointeur vers l'intérieur de la table globale. C'est crucial pour la sécurité du programme.
func TestLookupSmallReturnsCopy(t *testing.T) {
	n := uint64(10)
	val1 := lookupSmall(n)
	val2 := lookupSmall(n)

	// On vérifie que les valeurs sont égales.
	if val1.Cmp(val2) != 0 {
		t.Fatalf("lookupSmall(%d) returned different values on subsequent calls: %v, %v", n, val1, val2)
	}

	// On modifie la première valeur retournée.
	val1.Add(val1, big.NewInt(1))

	// On vérifie que la deuxième valeur n'a PAS été modifiée.
	if val1.Cmp(val2) == 0 {
		t.Errorf("lookupSmall returned a mutable value; modifying the returned value changed subsequent results")
	}

	// On vérifie également que la valeur dans la table globale n'a pas changé.
	originalValue := fibLookupTable[n]
	if val1.Cmp(originalValue) == 0 {
		t.Errorf("lookupSmall returned a pointer to the internal lookup table, which was then modified")
	}
}

// === Test 3: Utilisation d'un "Mock Object" pour tester le patron Décorateur ===

// mockCoreCalculator est un "faux" calculateur qui implémente l'interface `coreCalculator`.
// Il nous permet d'espionner les appels faits par le décorateur `FibCalculator`.
type mockCoreCalculator struct {
	// `t` est une référence à l'objet de test pour pouvoir signaler des erreurs depuis le mock.
	t *testing.T

	// Champs pour espionner l'appel
	called    bool
	calledWithN uint64

	// Champs pour contrôler le retour
	returnValue *big.Int
	returnError error
}

func (m *mockCoreCalculator) CalculateCore(_ context.Context, _ chan<- ProgressUpdate, _ int, n uint64, _ int) (*big.Int, error) {
	// On enregistre que la méthode a été appelée et avec quel argument.
	m.t.Logf("mockCoreCalculator.CalculateCore a été appelé avec n = %d", n)
	m.called = true
	m.calledWithN = n
	return m.returnValue, m.returnError
}

func (m *mockCoreCalculator) Name() string {
	return "MockCore"
}

// Test pour le "fast path" : `n` est petit, le `coreCalculator` ne doit PAS être appelé.
func TestFibCalculator_Calculate_FastPath(t *testing.T) {
	mockCore := &mockCoreCalculator{t: t}
	// On crée le décorateur `FibCalculator` en l'enveloppant autour de notre mock.
	calc := NewCalculator(mockCore)

	n := uint64(50) // n <= MaxFibUint64
	result, err := calc.Calculate(context.Background(), nil, 0, n, 0)

	if err != nil {
		t.Fatalf("Calculate() returned an unexpected error: %v", err)
	}
	// On vérifie que le résultat est correct (il doit venir de la lookup table).
	expected := lookupSmall(n)
	if result.Cmp(expected) != 0 {
		t.Errorf("Calculate() result = %v, want %v", result, expected)
	}

	// L'assertion la plus importante : le calculateur de cœur n'a pas été appelé.
	if mockCore.called {
		t.Error("coreCalculator.CalculateCore was called, but should not have been for the fast path")
	}
}

// Test pour le "slow path" : `n` est grand, le `coreCalculator` DOIT être appelé.
func TestFibCalculator_Calculate_SlowPath(t *testing.T) {
	expectedResult := big.NewInt(12345)
	mockCore := &mockCoreCalculator{
		t:           t,
		returnValue: expectedResult, // On configure le mock pour qu'il retourne une valeur spécifique.
		returnError: nil,
	}
	calc := NewCalculator(mockCore)

	n := uint64(100) // n > MaxFibUint64
	result, err := calc.Calculate(context.Background(), nil, 0, n, 0)

	if err != nil {
		t.Fatalf("Calculate() returned an unexpected error: %v", err)
	}
	if result.Cmp(expectedResult) != 0 {
		t.Errorf("Calculate() result = %v, want %v", result, expectedResult)
	}

	// Assertions les plus importantes : le calculateur de cœur a été appelé, et avec le bon argument.
	if !mockCore.called {
		t.Error("coreCalculator.CalculateCore was not called, but should have been for the slow path")
	}
	if mockCore.calledWithN != n {
		t.Errorf("coreCalculator.CalculateCore was called with n=%d, want n=%d", mockCore.calledWithN, n)
	}
}


// === Test 4: Test de la logique de communication (canaux) ===

func TestReportProgress(t *testing.T) {
	t.Run("Nil Channel", func(t *testing.T) {
		// Ce test vérifie simplement que la fonction ne panique pas si le canal est nil.
		// Il n'y a pas d'assertion explicite, le succès est l'absence de panique.
		reportProgress(nil, 0, 0.5)
	})

	t.Run("Successful Send", func(t *testing.T) {
		// On crée un canal avec un buffer pour pouvoir recevoir la valeur sans blocage.
		progressChan := make(chan ProgressUpdate, 1)
		reportProgress(progressChan, 1, 0.75)

		// On vérifie qu'on reçoit bien la mise à jour attendue.
		select {
		case update := <-progressChan:
			if update.CalculatorIndex != 1 {
				t.Errorf("got index %d, want 1", update.CalculatorIndex)
			}
			if update.Value != 0.75 {
				t.Errorf("got progress %f, want 0.75", update.Value)
			}
		case <-time.After(100 * time.Millisecond): // Timeout pour éviter que le test ne se bloque.
			t.Fatal("did not receive progress update on channel")
		}
	})

	t.Run("Non-Blocking on Full Channel", func(t *testing.T) {
		// On crée un canal SANS buffer. Il bloquera immédiatement à l'envoi
		// si personne n'est prêt à recevoir.
		progressChan := make(chan ProgressUpdate)

		// On exécute `reportProgress` dans une goroutine pour pouvoir vérifier
		// qu'elle ne reste pas bloquée.
		done := make(chan struct{})
		go func() {
			reportProgress(progressChan, 0, 0.5)
			close(done) // On signale que la fonction a terminé.
		}()

		// On attend, mais pas trop longtemps. Si la fonction `reportProgress` a
		// correctement utilisé `select` avec `default`, elle devrait se terminer
		// instantanément et fermer le canal `done`.
		select {
		case <-done:
			// C'est le cas de succès. La fonction a terminé sans bloquer.
		case <-time.After(100 * time.Millisecond):
			t.Fatal("reportProgress blocked on a full channel")
		}
	})
}


// === Test 5: Test du Pooling d'Objets (sync.Pool) ===

func TestStatePooling(t *testing.T) {
	// On récupère un premier état du pool.
	s1 := getState()

	// On vérifie qu'il est correctement initialisé (valeurs par défaut).
	if s1.f_k.Int64() != 0 || s1.f_k1.Int64() != 1 {
		t.Fatalf("Initial state from pool is incorrect: f_k=%v, f_k1=%v", s1.f_k, s1.f_k1)
	}

	// On modifie l'état.
	s1.f_k.SetInt64(100)
	s1.f_k1.SetInt64(200)

	// On le remet dans le pool.
	putState(s1)

	// On récupère un nouvel état (qui pourrait être le même objet recyclé).
	s2 := getState()

	// On vérifie qu'il a été RÉINITIALISÉ. C'est le test le plus important ici.
	// Si la réinitialisation n'avait pas lieu, s2 contiendrait encore 100 et 200.
	if s2.f_k.Int64() != 0 || s2.f_k1.Int64() != 1 {
		t.Errorf("State was not reset after being returned to the pool: f_k=%v, f_k1=%v", s2.f_k, s2.f_k1)
	}
}

func TestMatrixStatePooling(t *testing.T) {
    // État attendu après réinitialisation
    expectedRes := &matrix{a: big.NewInt(1), b: big.NewInt(0), c: big.NewInt(0), d: big.NewInt(1)} // Identité
    expectedP := &matrix{a: big.NewInt(1), b: big.NewInt(1), c: big.NewInt(1), d: big.NewInt(0)}   // Base Q

    // Fonction d'aide pour comparer deux matrices
    matrixEqual := func(m1, m2 *matrix) bool {
        return m1.a.Cmp(m2.a) == 0 && m1.b.Cmp(m2.b) == 0 && m1.c.Cmp(m2.c) == 0 && m1.d.Cmp(m2.d) == 0
    }
	matrixToString := func(m *matrix) string {
		return fmt.Sprintf("[[a:%v, b:%v], [c:%v, d:%v]]", m.a, m.b, m.c, m.d)
	}

    // Récupérer, vérifier l'état initial
    s1 := getMatrixState()
    if !matrixEqual(s1.res, expectedRes) {
        t.Errorf("Initial matrix state 'res' is incorrect.\nGot:  %s\nWant: %s", matrixToString(s1.res), matrixToString(expectedRes))
    }
    if !matrixEqual(s1.p, expectedP) {
        t.Errorf("Initial matrix state 'p' is incorrect.\nGot:  %s\nWant: %s", matrixToString(s1.p), matrixToString(expectedP))
    }

    // Modifier l'état
    s1.res.a.SetInt64(99)
    s1.p.a.SetInt64(99)

    // Remettre dans le pool
    putMatrixState(s1)

    // Récupérer un nouvel état (potentiellement recyclé)
    s2 := getMatrixState()

    // Vérifier qu'il a été réinitialisé
    if !matrixEqual(s2.res, expectedRes) {
        t.Errorf("Reset matrix state 'res' is incorrect.\nGot:  %s\nWant: %s", matrixToString(s2.res), matrixToString(expectedRes))
    }
    if !matrixEqual(s2.p, expectedP) {
        t.Errorf("Reset matrix state 'p' is incorrect.\nGot:  %s\nWant: %s", matrixToString(s2.p), matrixToString(expectedP))
    }
}

// Name() est une méthode triviale, mais pour être complet, on peut la tester.
func TestFibCalculator_Name(t *testing.T) {
	mockCore := &mockCoreCalculator{t: t}
	calc := NewCalculator(mockCore)
	if calc.Name() != "MockCore" {
		t.Errorf("Name() = %q, want %q", calc.Name(), "MockCore")
	}
}
