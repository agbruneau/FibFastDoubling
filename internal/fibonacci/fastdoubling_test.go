// EXPLICATION ACADÉMIQUE :
// Ce fichier teste l'implémentation de l'algorithme "Fast Doubling". Les tests
// couvrent non seulement l'exactitude des résultats, mais aussi des aspects
// plus complexes comme la gestion de l'annulation (context), le rapport de
// progression, et la sécurité des données retournées par le pool d'objets.

package fibonacci

import (
	"context"
	"math/big"
	"testing"
)

// === Test 1: Test d'exactitude (Table-Driven Test) ===
// C'est le test le plus fondamental : est-ce que la fonction calcule le bon nombre ?
// On utilise une table de tests pour vérifier plusieurs cas connus.
func TestFastDoubling_Correctness(t *testing.T) {
	// F(100) = 354224848179261915075
	f100, _ := new(big.Int).SetString("354224848179261915075", 10)
	// F(200) = 280571172992510140037611932413038677189525
	f200, _ := new(big.Int).SetString("280571172992510140037611932413038677189525", 10)

	testCases := []struct {
		n    uint64
		want *big.Int
		name string
	}{
		{0, big.NewInt(0), "F(0)"},
		{1, big.NewInt(1), "F(1)"},
		{2, big.NewInt(1), "F(2)"},
		{10, big.NewInt(55), "F(10)"},
		{20, big.NewInt(6765), "F(20)"},
		{93, fibLookupTable[93], "F(93)"}, // Test à la limite de la LUT
		{94, new(big.Int).Add(fibLookupTable[93], fibLookupTable[92]), "F(94)"}, // Test juste après la LUT
		{100, f100, "F(100)"},
		{200, f200, "F(200)"},
	}

	// Instance du calculateur à tester.
	calc := &OptimizedFastDoubling{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// On appelle la méthode à tester. Le seuil de parallélisme est mis à 0
			// pour que les tests soient reproductibles et n'activent pas le parallélisme.
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

// === Test 2: Test de l'annulation via le contexte ===
// Ce test vérifie que le calcul long peut être interrompu si le contexte est annulé.
func TestFastDoubling_ContextCancellation(t *testing.T) {
	calc := &OptimizedFastDoubling{}
	// On crée un contexte et on obtient sa fonction d'annulation.
	ctx, cancel := context.WithCancel(context.Background())

	// On annule le contexte immédiatement.
	cancel()

	// On lance le calcul avec un grand nombre. Sans la vérification du contexte,
	// ce calcul prendrait un certain temps.
	_, err := calc.CalculateCore(ctx, nil, 0, 1_000_000, 0)

	// On vérifie que la fonction s'est terminée rapidement avec la bonne erreur.
	if err == nil {
		t.Fatal("CalculateCore did not return an error on a canceled context")
	}
	if err != context.Canceled {
		t.Errorf("CalculateCore returned error %v, want %v", err, context.Canceled)
	}
}

// === Test 3: Test du rapport de progression ===
// On vérifie que les mises à jour de progression sont envoyées sur le canal.
func TestFastDoubling_ProgressReporting(t *testing.T) {
	calc := &OptimizedFastDoubling{}
	progressChan := make(chan ProgressUpdate, 20) // Canal avec buffer
	n := uint64(5000)

	_, err := calc.CalculateCore(context.Background(), progressChan, 1, n, 0)
	if err != nil {
		t.Fatalf("CalculateCore failed: %v", err)
	}
	close(progressChan) // On ferme le canal pour pouvoir itérer dessus.

	var updates []ProgressUpdate
	for update := range progressChan {
		updates = append(updates, update)
	}

	if len(updates) == 0 {
		t.Fatal("No progress updates were received")
	}

	// On vérifie que l'index du calculateur est correct.
	if updates[0].CalculatorIndex != 1 {
		t.Errorf("Expected calculator index 1, got %d", updates[0].CalculatorIndex)
	}

	// On vérifie que la progression est globalement croissante.
	lastProgress := 0.0
	for i, update := range updates {
		if update.Value < lastProgress {
			t.Errorf("Progress decreased at step %d: previous=%f, current=%f", i, lastProgress, update.Value)
		}
		lastProgress = update.Value
	}

	// On vérifie que la dernière mise à jour est bien 1.0 (terminé).
	if lastProgress != 1.0 {
		t.Errorf("Final progress update was %f, want 1.0", lastProgress)
	}
}

// === Test 4: Parallèle vs Séquentiel ===
// Ce test ne prouve pas que le code s'est exécuté en parallèle, mais il vérifie
// que les deux chemins de code (parallèle et séquentiel) produisent le même résultat.
func TestFastDoubling_ParallelVsSequential(t *testing.T) {
	calc := &OptimizedFastDoubling{}
	n := uint64(50000) // Un nombre assez grand pour que ses opérandes dépassent le seuil.

	// Exécution 1: Seuil très élevé, force l'exécution séquentielle.
	resultSeq, errSeq := calc.CalculateCore(context.Background(), nil, 0, n, 1_000_000)
	if errSeq != nil {
		t.Fatalf("Sequential execution failed: %v", errSeq)
	}

	// Exécution 2: Seuil bas, devrait activer l'exécution parallèle.
	resultPar, errPar := calc.CalculateCore(context.Background(), nil, 0, n, 128)
	if errPar != nil {
		t.Fatalf("Parallel execution failed: %v", errPar)
	}

	// Le résultat doit être identique dans les deux cas.
	if resultSeq.Cmp(resultPar) != 0 {
		t.Errorf("Sequential and parallel results do not match!\nSeq: %v\nPar: %v", resultSeq, resultPar)
	}
}

// === Test 5: Sécurité du résultat (pas de pointeur vers le pool) ===
// Ce test s'assure que la valeur retournée est une copie et non un pointeur vers
// la mémoire interne du pool, qui peut être recyclée et modifiée.
func TestFastDoubling_ResultIsCopy(t *testing.T) {
	calc := &OptimizedFastDoubling{}
	n := uint64(150)

	// Premier appel
	result1, err1 := calc.CalculateCore(context.Background(), nil, 0, n, 0)
	if err1 != nil {
		t.Fatalf("First call failed: %v", err1)
	}
	// On fait une copie de la valeur de result1 pour la comparaison future.
	expectedResult := new(big.Int).Set(result1)

	// Deuxième appel avec un nombre différent. Si `result1` pointait vers le pool,
	// son contenu serait maintenant écrasé par les calculs de F(151).
	_, err2 := calc.CalculateCore(context.Background(), nil, 0, n+1, 0)
	if err2 != nil {
		t.Fatalf("Second call failed: %v", err2)
	}

	// On vérifie que la valeur du premier résultat n'a pas changé.
	if result1.Cmp(expectedResult) != 0 {
		t.Errorf("Result from the first call was modified by the second call. The function likely returned a pointer to a pooled object.\nOriginal: %v\nModified: %v", expectedResult, result1)
	}
}

// === Test 6: Test de la méthode Name() ===
func TestFastDoubling_Name(t *testing.T) {
	calc := &OptimizedFastDoubling{}
	// On vérifie que le nom est bien celui attendu.
	// Ce test peut sembler trivial, mais il garantit que le nom qui identifie
	// l'algorithme dans l'interface utilisateur ne change pas accidentellement.
	expectedName := "OptimizedFastDoubling (3-Way-Parallel+ZeroAlloc+LUT)"
	if name := calc.Name(); name != expectedName {
		t.Errorf("Name() = %q, want %q", name, expectedName)
	}
}
