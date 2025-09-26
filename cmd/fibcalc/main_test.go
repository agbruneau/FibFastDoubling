//
// MODULE ACADÉMIQUE : TESTS D'INTÉGRATION ET DE LA RACINE DE COMPOSITION (MAIN)
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier de test montre comment tester le "Composition Root" (`main.go`) d'une
// application Go. Le défi est de tester des fonctions qui interagissent avec des
// éléments globaux (flags, `os.Args`, `os.Stdout`) et qui orchestrent le cycle de
// vie de l'application.
//
// CONCEPTS CLÉS DÉMONTRÉS :
//  1. TESTABILITÉ PAR CONCEPTION : Les fonctions `parseConfig` et `run` ont été
//     conçues pour être testables. Elles évitent l'état global en acceptant leurs
//     dépendances (arguments, `io.Writer`, `context`) comme paramètres, ce qui est
//     une application directe du principe d'Inversion de Dépendances (le 'D' de SOLID).
//  2. TESTS DE LA LOGIQUE DE PARSING : `TestParseConfig` utilise une approche de
//     test par table pour couvrir les cas de succès et d'erreur du parsing des
//     arguments de la ligne de commande, sans jamais dépendre de `os.Args`.
//  3. TESTS DE LA LOGIQUE D'EXÉCUTION (`run`) : `TestRunFunction` teste la logique
//     principale de l'application. Il vérifie que `run` produit la sortie attendue
//     et retourne les codes de sortie corrects en fonction des scénarios.
//  4. SIMULATION DE L'ENVIRONNEMENT :
//      - La sortie standard (`os.Stdout`) est remplacée par un `bytes.Buffer` pour
//        capturer et valider ce qui est affiché.
//      - Le `context` est utilisé pour simuler des conditions d'exécution spéciales
//        comme un timeout ou une annulation (Ctrl+C), permettant de tester les
//        chemins de code de "graceful shutdown".
//
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"example.com/fibcalc/internal/fibonacci"
)

// TestParseConfig valide la fonction de parsing et de validation de la configuration.
func TestParseConfig(t *testing.T) {
	// `io.Discard` est un `io.Writer` qui ignore toutes les écritures. C'est utile
	// ici pour ne pas polluer les logs de test avec les messages d'erreur de `flag.Usage()`.
	var discard bytes.Buffer

	testCases := []struct {
		name        string
		args        []string
		expectErr   bool
		expectedN   uint64
		expectedAlgo string
	}{
		{"Cas par défaut", []string{}, false, 100000000, "all"},
		{"Spécification de N", []string{"-n", "50"}, false, 50, "all"},
		{"Spécification de l'algo", []string{"-algo", "fast"}, false, 100000000, "fast"},
		{"Spécification de l'algo (majuscules)", []string{"-algo", "MATRIX"}, false, 100000000, "matrix"},
		{"Argument inconnu", []string{"-unknown"}, true, 0, ""},
		{"Algo inconnu", []string{"-algo", "invalid"}, true, 0, ""},
		{"Timeout négatif", []string{"-timeout", "-1s"}, true, 0, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := parseConfig("test", tc.args, &discard)

			if tc.expectErr {
				if err == nil {
					t.Error("Une erreur était attendue, mais aucune n'a été retournée.")
				}
			} else {
				if err != nil {
					t.Errorf("Une erreur inattendue a été retournée: %v", err)
				}
				if config.N != tc.expectedN {
					t.Errorf("config.N incorrect. Attendu: %d, Obtenu: %d", tc.expectedN, config.N)
				}
				if config.Algo != tc.expectedAlgo {
					t.Errorf("config.Algo incorrect. Attendu: %s, Obtenu: %s", tc.expectedAlgo, config.Algo)
				}
			}
		})
	}
}

// TestRunFunction teste la fonction d'orchestration principale `run`.
func TestRunFunction(t *testing.T) {

	// --- Cas 1: Exécution simple avec succès ---
	t.Run("SimpleSuccess", func(t *testing.T) {
		var buf bytes.Buffer
		config := AppConfig{N: 10, Algo: "fast", Timeout: 1 * time.Minute, Threshold: fibonacci.DefaultParallelThreshold}

		exitCode := run(context.Background(), config, &buf)

		if exitCode != ExitSuccess {
			t.Errorf("Code de sortie incorrect. Attendu: %d, Obtenu: %d", ExitSuccess, exitCode)
		}
		output := buf.String()
		if !strings.Contains(output, "F(10) = 55") {
			t.Errorf("La sortie ne contient pas le résultat attendu 'F(10) = 55'. Sortie:\n%s", output)
		}
	})

	// --- Cas 2: Comparaison avec succès ---
	t.Run("ComparisonSuccess", func(t *testing.T) {
		var buf bytes.Buffer
		config := AppConfig{N: 20, Algo: "all", Timeout: 1 * time.Minute, Threshold: fibonacci.DefaultParallelThreshold}

		exitCode := run(context.Background(), config, &buf)

		if exitCode != ExitSuccess {
			t.Errorf("Code de sortie incorrect. Attendu: %d, Obtenu: %d", ExitSuccess, exitCode)
		}
		output := buf.String()
		if !strings.Contains(output, "Statut Global : Succès") {
			t.Errorf("La sortie ne contient pas le statut de succès global. Sortie:\n%s", output)
		}
		if !strings.Contains(output, "F(20) = 6,765") {
			t.Errorf("La sortie ne contient pas le résultat attendu 'F(20) = 6,765'. Sortie:\n%s", output)
		}
	})

	// --- Cas 3: Test de timeout ---
	t.Run("Timeout", func(t *testing.T) {
		var buf bytes.Buffer
		// On choisit un N très grand et un timeout très court pour forcer une erreur de timeout.
		config := AppConfig{N: 100_000_000, Algo: "fast", Timeout: 1 * time.Millisecond, Threshold: fibonacci.DefaultParallelThreshold}

		// On utilise un contexte de base, le timeout est géré par la fonction `run` elle-même.
		exitCode := run(context.Background(), config, &buf)

		if exitCode != ExitErrorTimeout {
			t.Errorf("Code de sortie incorrect. Attendu: %d, Obtenu: %d", ExitErrorTimeout, exitCode)
		}
		output := buf.String()
		if !strings.Contains(output, "Échec (Timeout)") {
			t.Errorf("La sortie n'indique pas une erreur de timeout. Sortie:\n%s", output)
		}
	})

	// --- Cas 4: Test d'annulation par le contexte ---
	t.Run("ContextCancellation", func(t *testing.T) {
		var buf bytes.Buffer
		config := AppConfig{N: 100_000_000, Algo: "fast", Timeout: 1 * time.Minute, Threshold: fibonacci.DefaultParallelThreshold}

		// EXPLICATION ACADÉMIQUE : Simulation d'une Annulation (Ctrl+C)
		// On crée un contexte qui peut être annulé manuellement.
		// `context.WithCancel` retourne le contexte et une fonction `cancel`.
		ctx, cancel := context.WithCancel(context.Background())

		// On appelle `cancel()` immédiatement. La fonction `run` devrait détecter
		// cette annulation dès le début de son exécution.
		cancel()

		exitCode := run(ctx, config, &buf)

		if exitCode != ExitErrorCanceled {
			t.Errorf("Code de sortie incorrect. Attendu: %d, Obtenu: %d", ExitErrorCanceled, exitCode)
		}
		output := buf.String()
		if !strings.Contains(output, "Statut : Annulé") {
			t.Errorf("La sortie n'indique pas une annulation. Sortie:\n%s", output)
		}
	})
}