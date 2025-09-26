//
// MODULE ACADÉMIQUE : TESTS UNITAIRES ET BENCHMARKS EN GO
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier de test démontre les meilleures pratiques pour tester du code Go,
// en particulier pour des modules complexes et orientés performance.
//
// CONCEPTS CLÉS DÉMONTRÉS :
//  1. TESTS DE TABLE (TABLE-DRIVEN TESTS) : Le test `TestFibonacciCalculators` utilise
//     une structure de données (un slice de structs) pour définir un ensemble complet
//     de cas de test. Cette approche rend les tests plus clairs, plus faciles à maintenir
//     et à étendre.
//  2. SOUS-TESTS (SUB-TESTS) AVEC `t.Run()` : Chaque algorithme et chaque cas de test
//     est exécuté dans son propre sous-test. Cela offre plusieurs avantages :
//      - ISOLATION : Un échec dans un sous-test ne stoppe pas les autres.
//      - CLARTÉ : Le nom du sous-test (`t.Run("Algo/N=...", ...)` indique précisément
//        quel cas a échoué.
//      - SÉLECTIVITÉ : On peut exécuter un sous-test spécifique avec `go test -run <pattern>`.
//  3. TESTS DE PERFORMANCE (BENCHMARKS) : Les fonctions préfixées par `Benchmark`
//     utilisent le framework de benchmark intégré de Go (`testing.B`). Elles mesurent
//     non seulement le temps d'exécution mais aussi les allocations mémoire, ce qui est
//     crucial pour valider les optimisations "zéro-allocation".
//  4. TESTS D'INTÉGRATION DE BAS NIVEAU : Le test `TestLookupTableImmutability` vérifie
//     une propriété architecturale critique (l'immuabilité de la LUT), qui n'est pas
//     directement liée à un algorithme mais au comportement correct du module dans son ensemble.
//  5. GESTION DES DÉPENDANCES DE TEST : Le test utilise les interfaces publiques
//     (`Calculator`) pour tester les implémentations, respectant ainsi l'encapsulation
//     du module.
//
package fibonacci

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"
)

// knownFibResults est une "source de vérité" contenant des valeurs de Fibonacci
// pré-calculées et vérifiées. Elle est utilisée comme référence pour valider
// l'exactitude de nos algorithmes.
var knownFibResults = []struct {
	n      uint64
	result string
}{
	{0, "0"},
	{1, "1"},
	{2, "1"},
	{10, "55"},
	{20, "6765"},
	{50, "12586269025"},
	{92, "7540113804746346429"},
	{93, "12200160415121876738"}, // Dépasse uint64
	{100, "354224848179261915075"},
	{200, "280571172992510140037611932413038677189525"},
	{1000, "43466557686937456435688527675040625802564660517371780402481729089536555417949051890403879840079255169295922593080322634775209689623239873322471161642996440906533187938298969649928516003704476137795166849228875"},
	{2000, "4224696333392304878706725602341482782579852840250681098010280137314308584370130707224123599639141511088446087538909603607640194711643596029271983312598737326253555802606991585915229492453904998722256795316982874482472992263901833716778060607011615497886719879858311468870876264597369086722884023654422295243347964480139515349562972087652656069529806499841977448720155612802665404554171717881930324025204312082516817125"},
}

// TestFibonacciCalculators est un test de table complet qui valide toutes les implémentations
// de l'interface `Calculator` contre la source de vérité `knownFibResults`.
func TestFibonacciCalculators(t *testing.T) {
	// EXPLICATION ACADÉMIQUE : Le `context.Background()` est utilisé comme contexte
	// racine pour les tests. Pour des tests plus avancés, on pourrait utiliser
	// `context.WithTimeout` pour s'assurer qu'un test ne reste pas bloqué indéfiniment.
	ctx := context.Background()

	// On récupère les implémentations de `Calculator` à tester.
	// C'est le même mécanisme que `main.go`, ce qui garantit que nous testons
	// exactement ce que l'application utilise.
	calculators := map[string]Calculator{
		"FastDoubling": NewCalculator(&OptimizedFastDoubling{}),
		"MatrixExp":    NewCalculator(&MatrixExponentiation{}),
	}

	for name, calc := range calculators {
		// Démarrage d'un sous-test pour chaque algorithme.
		// `t.Run` permet d'isoler les tests et de fournir des rapports plus clairs.
		t.Run(name, func(t *testing.T) {
			for _, testCase := range knownFibResults {
				// Démarrage d'un sous-test pour chaque valeur de n.
				t.Run(fmt.Sprintf("N=%d", testCase.n), func(t *testing.T) {
					// `t.Parallel()` marque ce test comme pouvant être exécuté en parallèle
					// avec d'autres sous-tests du même niveau. Go Test Runner s'occupe de la planification.
					t.Parallel()

					// On attend un `*big.Int` de la part de `knownFibResults`.
					expected := new(big.Int)
					expected.SetString(testCase.result, 10)

					// On exécute le calcul. Le canal de progression est `nil` car non pertinent pour ce test.
					result, err := calc.Calculate(ctx, nil, 0, testCase.n, DefaultParallelThreshold)

					// --- VÉRIFICATIONS (ASSERTIONS) ---
					if err != nil {
						// `t.Fatalf` enregistre l'erreur et arrête l'exécution de ce sous-test immédiatement.
						t.Fatalf("Le calcul a retourné une erreur inattendue : %v", err)
					}
					if result == nil {
						t.Fatal("Le calcul a retourné un résultat nil sans erreur")
					}
					// `result.Cmp(expected)` est la manière idiomatique de comparer des `big.Int`.
					// Elle retourne 0 si les nombres sont égaux.
					if result.Cmp(expected) != 0 {
						// `t.Errorf` enregistre une erreur mais continue l'exécution du test.
						// Utile si on veut voir plusieurs erreurs dans le même test.
						t.Errorf("Résultat incorrect.\nAttendu: %s\nObtenu : %s", expected.String(), result.String())
					}
				})
			}
		})
	}
}

// TestLookupTableImmutability vérifie une propriété de sécurité critique :
// que la table de consultation (LUT) retourne des copies et non des pointeurs
// vers son état interne, afin d'empêcher des modifications externes accidentelles.
func TestLookupTableImmutability(t *testing.T) {
	// On récupère F(10) depuis la LUT.
	val1 := lookupSmall(10)
	expected := big.NewInt(55)
	if val1.Cmp(expected) != 0 {
		t.Fatalf("La valeur initiale de F(10) est incorrecte. Attendu 55, obtenu %s", val1.String())
	}

	// On tente de modifier la valeur obtenue.
	// Si `lookupSmall` a incorrectement retourné un pointeur direct vers l'entrée
	// de la table, cette modification corrompra la table globale.
	val1.Add(val1, big.NewInt(1)) // val1 devient 56

	// On récupère à nouveau F(10).
	val2 := lookupSmall(10)

	// La valeur re-récupérée doit TOUJOURS être 55. Si elle est 56, cela signifie
	// que notre modification a "fuité" dans la LUT, ce qui est un bug critique.
	if val2.Cmp(expected) != 0 {
		t.Fatalf("Violation d'immuabilité ! La LUT a été modifiée par un appelant externe. F(10) devrait être 55, mais est maintenant %s", val2.String())
	}
	if val1.Cmp(val2) == 0 {
		t.Fatal("Les deux valeurs retournées ne devraient pas être égales après modification de la première.")
	}
}

// TestNilCoreCalculatorPanic vérifie que la factory `NewCalculator` panique bien
// si on lui passe un `coreCalculator` nil, ce qui est un contrat de conception important.
func TestNilCoreCalculatorPanic(t *testing.T) {
	// `defer` et `recover` est le idiome Go pour tester les paniques.
	defer func() {
		if r := recover(); r == nil {
			// Si `recover` retourne `nil`, cela signifie qu'aucune panique n'a eu lieu.
			t.Error("NewCalculator devrait paniquer avec un core nil, mais ne l'a pas fait.")
		}
	}()
	// Cette ligne devrait déclencher une panique.
	_ = NewCalculator(nil)
}

// TestProgressReporter vérifie que les calculateurs rapportent leur progression
// et terminent avec une progression de 1.0.
func TestProgressReporter(t *testing.T) {
	calculators := map[string]Calculator{
		"FastDoubling": NewCalculator(&OptimizedFastDoubling{}),
		"MatrixExp":    NewCalculator(&MatrixExponentiation{}),
	}

	for name, calc := range calculators {
		t.Run(name, func(t *testing.T) {
			progressChan := make(chan ProgressUpdate, 200)
			var lastProgress float64
			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				defer wg.Done()
				for update := range progressChan {
					if update.Value < lastProgress {
						t.Errorf("La progression a diminué, ce qui est invalide. Précédent: %f, Actuel: %f", lastProgress, update.Value)
					}
					lastProgress = update.Value
				}
			}()

			_, err := calc.Calculate(context.Background(), progressChan, 0, 10000, DefaultParallelThreshold)
			close(progressChan)
			wg.Wait()

			if err != nil {
				t.Fatalf("Le calcul a échoué: %v", err)
			}

			if lastProgress != 1.0 {
				t.Errorf("La progression finale devrait être 1.0, mais est %f", lastProgress)
			}
		})
	}
}

// TestContextCancellation vérifie que les calculs s'arrêtent bien lorsqu'un
// contexte est annulé.
func TestContextCancellation(t *testing.T) {
	// On choisit un nombre très grand pour que le calcul soit long.
	const n = 100_000_000

	calculators := map[string]Calculator{
		"FastDoubling": NewCalculator(&OptimizedFastDoubling{}),
		"MatrixExp":    NewCalculator(&MatrixExponentiation{}),
	}

	for name, calc := range calculators {
		t.Run(name, func(t *testing.T) {
			// On crée un contexte qui sera annulé après un court délai.
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := calc.Calculate(ctx, nil, 0, n, DefaultParallelThreshold)

			// On s'attend à une erreur de type "context deadline exceeded".
			if err == nil {
				t.Fatal("Le calcul aurait dû être annulé par le contexte, mais il s'est terminé sans erreur.")
			}
			if err != context.DeadlineExceeded {
				t.Errorf("Erreur inattendue. Attendu: %v, Obtenu: %v", context.DeadlineExceeded, err)
			}
		})
	}
}

// --- BENCHMARKS ---

// runBenchmark est une fonction d'aide pour structurer les benchmarks.
func runBenchmark(b *testing.B, calc Calculator, n uint64) {
	ctx := context.Background()
	// `b.N` est une variable spéciale fournie par le framework de benchmark.
	// Le testeur ajuste `b.N` dynamiquement pour que le benchmark dure un temps
	// statistiquement significatif.
	for i := 0; i < b.N; i++ {
		// On passe un canal de progression pour simuler des conditions réelles.
		// Pour un benchmark pur, on pourrait le mettre à `nil` pour enlever ce léger surcoût.
		progressChan := make(chan ProgressUpdate, 10)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range progressChan {
				// On vide le canal pour ne pas bloquer le producteur.
			}
		}()

		// L'appel à la fonction dont on veut mesurer la performance.
		_, _ = calc.Calculate(ctx, progressChan, 0, n, DefaultParallelThreshold)

		close(progressChan)
		wg.Wait()
	}
}

func BenchmarkFastDoubling1M(b *testing.B) {
	runBenchmark(b, NewCalculator(&OptimizedFastDoubling{}), 1_000_000)
}

func BenchmarkMatrixExp1M(b *testing.B) {
	runBenchmark(b, NewCalculator(&MatrixExponentiation{}), 1_000_000)
}

func BenchmarkFastDoubling10M(b *testing.B) {
	runBenchmark(b, NewCalculator(&OptimizedFastDoubling{}), 10_000_000)
}

func BenchmarkMatrixExp10M(b *testing.B) {
	runBenchmark(b, NewCalculator(&MatrixExponentiation{}), 10_000_000)
}