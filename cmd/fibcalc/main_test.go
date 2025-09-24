// EXPLICATION ACADÉMIQUE :
// Ce fichier est un test d'intégration pour le package `main`. Contrairement aux
// tests unitaires qui se concentrent sur une seule fonction ou une seule structure,
// les tests d'intégration vérifient que plusieurs composants du système fonctionnent
// correctement ensemble. Ici, on teste la fonction `run()`, qui est le "cerveau"
// de l'application et orchestre les paquets `fibonacci` et `cli`.
// On ne teste pas `main()` directement car elle appelle `os.Exit`, ce qui terminerait
// le processus de test. La fonction `run()` a été conçue pour être testable en
// retournant un code de sortie au lieu d'appeler `os.Exit`.

package main

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"example.com/fibcalc/internal/fibonacci"
)

// === Test 1: Scénarios de base pour l'exécution d'un seul algorithme ===

func TestRun_SingleAlgorithm_Success(t *testing.T) {
	// Configuration pour un calcul simple et rapide.
	config := AppConfig{
		N:         40, // Assez petit pour être rapide, mais non trivial.
		Algo:      "fast",
		Timeout:   10 * time.Second,
		Threshold: fibonacci.DefaultParallelThreshold,
	}
	var out bytes.Buffer

	// On exécute la fonction `run` et on vérifie le code de sortie.
	exitCode := run(context.Background(), config, &out)
	if exitCode != ExitSuccess {
		t.Errorf("Expected exit code %d, got %d", ExitSuccess, exitCode)
	}

	// On vérifie que la sortie contient des informations attendues.
	output := out.String()
	if !strings.Contains(output, "Algorithme : OptimizedFastDoubling") {
		t.Error("Output should mention the algorithm used.")
	}
	if !strings.Contains(output, "F(40) = 102334155") { // F(40) est une valeur connue.
		t.Error("Output should contain the correct final result.")
	}
}

func TestRun_SingleAlgorithm_Timeout(t *testing.T) {
	// Configuration conçue pour échouer par timeout.
	config := AppConfig{
		N:         10_000_000, // Un très grand nombre qui prendra du temps.
		Algo:      "fast",
		Timeout:   1 * time.Millisecond, // Un timeout très court.
		Threshold: fibonacci.DefaultParallelThreshold,
	}
	var out bytes.Buffer

	exitCode := run(context.Background(), config, &out)
	if exitCode != ExitErrorTimeout {
		t.Errorf("Expected exit code %d for timeout, got %d", ExitErrorTimeout, exitCode)
	}

	output := out.String()
	if !strings.Contains(output, "Statut : Échec (Timeout)") {
		t.Error("Output should indicate a timeout failure.")
	}
}

func TestRun_SingleAlgorithm_Cancelled(t *testing.T) {
	config := AppConfig{
		N:       10_000_000,
		Algo:    "fast",
		Timeout: 10 * time.Second,
	}
	var out bytes.Buffer
	// On crée un contexte qui est déjà annulé.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exitCode := run(ctx, config, &out)
	if exitCode != ExitErrorCanceled {
		t.Errorf("Expected exit code %d for cancellation, got %d", ExitErrorCanceled, exitCode)
	}

	output := out.String()
	if !strings.Contains(output, "Statut : Annulé") {
		t.Error("Output should indicate a cancellation.")
	}
}

func TestRun_UnknownAlgorithm(t *testing.T) {
	config := AppConfig{
		N:    100,
		Algo: "nonexistent", // Un algorithme qui n'est pas dans le registre.
	}
	var out bytes.Buffer

	exitCode := run(context.Background(), config, &out)
	if exitCode != ExitErrorGeneric {
		t.Errorf("Expected exit code %d for unknown algo, got %d", ExitErrorGeneric, exitCode)
	}

	output := out.String()
	if !strings.Contains(output, "Erreur : algorithme 'nonexistent' inconnu.") {
		t.Error("Output should contain the unknown algorithm error message.")
	}
}


// === Test 2: Scénarios pour le mode de comparaison ("all") ===

func TestRun_AllAlgorithms_Success(t *testing.T) {
	config := AppConfig{
		N:       95, // Un nombre juste au-delà de la LUT, qui force un calcul réel.
		Algo:    "all",
		Timeout: 20 * time.Second,
	}
	var out bytes.Buffer

	exitCode := run(context.Background(), config, &out)
	if exitCode != ExitSuccess {
		t.Errorf("Expected exit code %d for 'all' mode success, got %d", ExitSuccess, exitCode)
	}

	output := out.String()
	// On vérifie que les deux algorithmes ont été exécutés avec succès.
	if !strings.Contains(output, "OptimizedFastDoubling") || !strings.Contains(output, "MatrixExponentiation") {
		t.Error("Output should list both algorithms.")
	}
	if strings.Count(output, "Statut: Succès") != 2 {
		t.Errorf("Expected 2 success statuses, found %d", strings.Count(output, "Statut: Succès"))
	}
	// On vérifie que la validation finale a réussi.
	if !strings.Contains(output, "Statut Global : Succès. Tous les résultats valides sont identiques.") {
		t.Error("Output should show the global success and validation message.")
	}
}

// mockCalculator est un mock qui nous permet de simuler des comportements spécifiques,
// comme retourner une erreur ou un mauvais résultat.
type mockCalculator struct {
	name        string
	returnValue *big.Int
	returnError error
}

func (m *mockCalculator) Calculate(ctx context.Context, progressChan chan<- fibonacci.ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	// On simule une vérification de contexte pour que le mock se comporte bien.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return m.returnValue, nil
}

func (m *mockCalculator) Name() string {
	return m.name
}

func TestRun_AllAlgorithms_Mismatch(t *testing.T) {
	// === SETUP: Remplacer un vrai calculateur par un mock ===
	// On sauvegarde le calculateur original pour le restaurer après le test.
	originalMatrixCalc := calculatorRegistry["matrix"]
	// On injecte notre mock qui retourne un résultat incorrect.
	calculatorRegistry["matrix"] = &mockCalculator{
		name:        "CorruptedMatrix",
		returnValue: big.NewInt(99999), // Un résultat manifestement faux.
		returnError: nil,
	}
	// `defer` garantit que le calculateur original est restauré même si le test panique.
	// C'est crucial pour ne pas affecter les autres tests.
	defer func() {
		calculatorRegistry["matrix"] = originalMatrixCalc
	}()

	// === TEST ===
	config := AppConfig{
		N:       100,
		Algo:    "all",
		Timeout: 20 * time.Second,
	}
	var out bytes.Buffer

	exitCode := run(context.Background(), config, &out)

	// === ASSERTIONS ===
	if exitCode != ExitErrorMismatch {
		t.Errorf("Expected exit code %d for mismatch, got %d", ExitErrorMismatch, exitCode)
	}

	output := out.String()
	if !strings.Contains(output, "Statut Global : Échec Critique ! Les algorithmes ont produit des résultats différents.") {
		t.Error("Output should contain the critical mismatch error.")
	}
}

func TestRun_AllAlgorithms_OneFails(t *testing.T) {
	// === SETUP: Remplacer un vrai calculateur par un mock qui retourne une erreur ===
	originalMatrixCalc := calculatorRegistry["matrix"]
	calculatorRegistry["matrix"] = &mockCalculator{
		name:        "FailingMatrix",
		returnError: errors.New("simulated calculation error"),
	}
	defer func() {
		calculatorRegistry["matrix"] = originalMatrixCalc
	}()

	// === TEST ===
	config := AppConfig{
		N:       100,
		Algo:    "all",
		Timeout: 20 * time.Second,
	}
	var out bytes.Buffer

	exitCode := run(context.Background(), config, &out)

	// === ASSERTIONS ===
	// Même si un algo échoue, si l'autre réussit, le programme dans son ensemble
	// est considéré comme un succès car on a pu obtenir un résultat valide.
	if exitCode != ExitSuccess {
		t.Errorf("Expected exit code %d, got %d", ExitSuccess, exitCode)
	}

	output := out.String()
	if !strings.Contains(output, "Statut: Échec (simulated calculation error)") {
		t.Error("Output should show the failure status for the mocked calculator.")
	}
	if !strings.Contains(output, "Statut Global : Succès. Tous les résultats valides sont identiques.") {
		t.Error("Output should still show global success as one algorithm succeeded.")
	}
}
