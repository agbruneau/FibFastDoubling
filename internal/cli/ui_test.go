// EXPLICATION ACADÉMIQUE :
// Ce fichier de test pour le package `cli` montre comment tester du code qui
// interagit avec des I/O (comme le terminal) et qui est concurrent (utilise des
// goroutines et des canaux). La clé est "d'injecter les dépendances" : au lieu
// que le code écrive directement sur `os.Stdout`, il écrit sur une interface
// `io.Writer`. En test, on peut fournir un `bytes.Buffer` qui implémente cette
// interface, nous permettant de capturer et d'inspecter la sortie.

package cli

import (
	"bytes"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/fibcalc/internal/fibonacci"
)

// === Test 1: Test d'une fonction pure (`progressBar`) ===
// `progressBar` est une fonction "pure" : sa sortie ne dépend que de ses entrées,
// sans effets de bord. C'est le type de fonction le plus simple à tester.
// On utilise un "table-driven test" pour couvrir plusieurs cas de manière concise.
func TestProgressBar(t *testing.T) {
	testCases := []struct {
		name     string
		progress float64
		length   int
		want     string
	}{
		{"Zero progress", 0.0, 10, "          "},
		{"Half progress", 0.5, 10, "■■■■■     "},
		{"Full progress", 1.0, 10, "■■■■■■■■■■"},
		{"Partial progress", 0.25, 20, "■■■■■               "},
		{"Progress over 100%", 1.5, 10, "■■■■■■■■■■"}, // Doit être limité à 100%
		{"Negative progress", -0.5, 10, "          "}, // Doit être limité à 0%
		{"Zero length", 0.5, 0, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := progressBar(tc.progress, tc.length)
			if got != tc.want {
				t.Errorf("progressBar(%.2f, %d) = %q, want %q", tc.progress, tc.length, got, tc.want)
			}
		})
	}
}

// === Test 2: Test d'une fonction avec I/O (`DisplayResult`) ===
// On teste que la fonction formate correctement sa sortie en la capturant dans un buffer.
func TestDisplayResult(t *testing.T) {
	result := new(big.Int)
	result.SetString("123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890", 10)
	smallResult := big.NewInt(12345)
	duration := 123 * time.Millisecond

	t.Run("Large number, not verbose", func(t *testing.T) {
		var buf bytes.Buffer
		DisplayResult(result, 1000, duration, false, &buf)
		output := buf.String()

		// On vérifie que la sortie contient les bonnes informations.
		if !strings.Contains(output, "Durée d'exécution : 123ms") {
			t.Error("Output should contain the duration")
		}
		if !strings.Contains(output, "Nombre de Chiffres Décimaux : 90") {
			t.Error("Output should contain the number of digits")
		}
		// On vérifie que le résultat a bien été tronqué.
		expectedSubtring := "(Tronqué) = 12345678901234567890...12345678901234567890"
		if !strings.Contains(output, expectedSubtring) {
			t.Errorf("Output should contain the truncated result: %q", expectedSubtring)
		}
		if strings.Contains(output, result.String()) {
			t.Error("Output should not contain the full result string")
		}
	})

	t.Run("Large number, verbose", func(t *testing.T) {
		var buf bytes.Buffer
		DisplayResult(result, 1000, duration, true, &buf)
		output := buf.String()

		// En mode verbeux, le résultat complet doit être présent.
		if !strings.Contains(output, result.String()) {
			t.Error("Verbose output should contain the full result string")
		}
		if strings.Contains(output, "(Tronqué)") {
			t.Error("Verbose output should not be truncated")
		}
	})

	t.Run("Small number", func(t *testing.T) {
		var buf bytes.Buffer
		DisplayResult(smallResult, 10, duration, false, &buf)
		output := buf.String()

		// Un petit nombre ne doit jamais être tronqué.
		if !strings.Contains(output, "F(10) = 12345") {
			t.Error("Small number should be displayed fully")
		}
		if strings.Contains(output, "(Tronqué)") {
			t.Error("Small number output should not be truncated")
		}
	})
}


// === Test 3: Test d'une fonction concurrente (`DisplayAggregateProgress`) ===
// C'est le test le plus complexe. Il simule le cycle de vie complet de la goroutine.
func TestDisplayAggregateProgress(t *testing.T) {
	var buf bytes.Buffer // Capture la sortie (le "terminal")
	var wg sync.WaitGroup
	progressChan := make(chan fibonacci.ProgressUpdate)

	// On lance la fonction à tester dans sa propre goroutine, comme dans l'application réelle.
	wg.Add(1)
	go DisplayAggregateProgress(&wg, progressChan, 2, &buf)

	// Fonction d'aide pour vérifier la dernière ligne de sortie (ignorant le `\r`)
	getLastLine := func() string {
		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\r")
		if len(lines) == 0 {
			return ""
		}
		return lines[len(lines)-1]
	}

	// 1. Envoyer une mise à jour pour le premier calculateur
	progressChan <- fibonacci.ProgressUpdate{CalculatorIndex: 0, Value: 0.25}
	// Laisser un peu de temps au ticker pour se déclencher et afficher la barre
	time.Sleep(150 * time.Millisecond)
	line1 := getLastLine()
	// La moyenne est (0.25 + 0.0) / 2 = 12.5%
	if !strings.Contains(line1, "12.50%") {
		t.Errorf("Expected progress bar to show 12.50%%, got %q", line1)
	}

	// 2. Envoyer une mise à jour pour le second calculateur
	progressChan <- fibonacci.ProgressUpdate{CalculatorIndex: 1, Value: 0.75}
	time.Sleep(150 * time.Millisecond)
	line2 := getLastLine()
	// La moyenne est (0.25 + 0.75) / 2 = 50.0%
	if !strings.Contains(line2, "50.00%") {
		t.Errorf("Expected progress bar to show 50.00%%, got %q", line2)
	}

	// 3. Fermer le canal pour signaler la fin
	close(progressChan)
	// Attendre que la goroutine se termine proprement
	wg.Wait()

	// 4. Vérifier la sortie finale
	finalOutput := getLastLine()
	// La barre finale doit être à 100%
	if !strings.Contains(finalOutput, "100.00%") || !strings.Contains(finalOutput, strings.Repeat("■", 30)) {
		t.Errorf("Expected final output to be a full 100%% bar, got %q", finalOutput)
	}
	// Et la sortie complète doit se terminer par une nouvelle ligne pour ne pas écraser la barre finale.
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("Final output should end with a newline, got %q", buf.String())
	}
}
