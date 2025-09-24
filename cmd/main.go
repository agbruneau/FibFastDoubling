package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	// Importation pour la concurrence structurée.
	"golang.org/x/sync/errgroup"

	// Adapter les imports au nom du module réel (e.g., example.com/fibcalc)
	"example.com/fibcalc/internal/cli"
	"example.com/fibcalc/internal/fibonacci"
)

const (
	ExitSuccess       = 0
	ExitErrorGeneric  = 1
	ExitErrorTimeout  = 2
	ExitErrorCanceled = 130
	ExitErrorMismatch = 3
)

type AppConfig struct {
	N         uint64
	Verbose   bool
	Timeout   time.Duration
	Algo      string
	Threshold int
}

var calculatorRegistry = map[string]fibonacci.Calculator{
	"fast":   fibonacci.NewCalculator(&fibonacci.OptimizedFastDoubling{}),
	"matrix": fibonacci.NewCalculator(&fibonacci.MatrixExponentiation{}),
}

func main() {
	nFlag := flag.Uint64("n", 100000000, "L'indice 'n' de la séquence de Fibonacci à calculer.")
	verboseFlag := flag.Bool("v", false, "Affiche le résultat complet.")
	timeoutFlag := flag.Duration("timeout", 5*time.Minute, "Délai maximum (ex: 30s, 1m).")
	algoFlag := flag.String("algo", "all", "Algorithme : 'fast', 'matrix', ou 'all'.")
	thresholdFlag := flag.Int("threshold", fibonacci.DefaultParallelThreshold, "Seuil (en bits) pour activer la multiplication parallèle.")

	flag.Parse()

	config := AppConfig{
		N:         *nFlag,
		Verbose:   *verboseFlag,
		Timeout:   *timeoutFlag,
		Algo:      *algoFlag,
		Threshold: *thresholdFlag,
	}

	exitCode := run(context.Background(), config, os.Stdout)
	os.Exit(exitCode)
}

type CalculationResult struct {
	Name     string
	Result   *big.Int
	Duration time.Duration
	Err      error
}

func run(ctx context.Context, config AppConfig, out io.Writer) int {
	// --- Gestion Robuste du Contexte (Timeout + Signaux OS) ---
	ctx, cancelTimeout := context.WithTimeout(ctx, config.Timeout)
	defer cancelTimeout()

	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// Affichage de la configuration initiale.
	fmt.Fprintf(out, "--- Configuration ---\n")
	fmt.Fprintf(out, "Calcul de F(%d).\n", config.N)
	fmt.Fprintf(out, "Système : CPU Cores=%d | Go Runtime=%s\n", runtime.NumCPU(), runtime.Version())
	fmt.Fprintf(out, "Paramètres : Timeout=%s | Parallel Threshold=%d bits\n", config.Timeout, config.Threshold)

	// --- Sélection des Calculateurs ---
	var calculatorsToRun []fibonacci.Calculator
	algo := strings.ToLower(config.Algo)

	if algo == "all" {
		fmt.Fprintf(out, "Mode : Comparaison (Exécution parallèle).\n")
		keys := make([]string, 0, len(calculatorRegistry))
		for k := range calculatorRegistry {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			calculatorsToRun = append(calculatorsToRun, calculatorRegistry[k])
		}
	} else {
		calculator, ok := calculatorRegistry[algo]
		if !ok {
			fmt.Fprintf(out, "Erreur : algorithme '%s' inconnu.\n", config.Algo)
			return ExitErrorGeneric
		}
		fmt.Fprintf(out, "Mode : Simple exécution.\nAlgorithme : %s\n", calculator.Name())
		calculatorsToRun = append(calculatorsToRun, calculator)
	}

	fmt.Fprintf(out, "\n--- Exécution ---\n")

	// --- Exécution et Analyse ---
	results := executeCalculations(ctx, calculatorsToRun, config, out)

	if len(results) == 1 {
		res := results[0]
		fmt.Fprintln(out, "\n--- Résultat Final ---")
		if res.Err != nil {
			return handleCalculationError(res.Err, res.Duration, config.Timeout, out)
		}
		cli.DisplayResult(res.Result, config.N, res.Duration, config.Verbose, out)
		return ExitSuccess
	}

	return analyzeComparisonResults(results, config, out)
}

// executeCalculations orchestre l'exécution parallèle.
// REFACTORISATION MAJEURE : Utilisation de errgroup pour remplacer la synchronisation manuelle complexe
// (3 WaitGroups et canaux proxy) par une concurrence structurée robuste et simple.
func executeCalculations(ctx context.Context, calculators []fibonacci.Calculator, config AppConfig, out io.Writer) []CalculationResult {
	// Initialisation de errgroup lié au contexte principal.
	g, ctx := errgroup.WithContext(ctx)

	results := make([]CalculationResult, len(calculators))
	// Canal central d'agrégation de la progression.
	progressChan := make(chan fibonacci.ProgressUpdate, len(calculators)*10)

	// --- Lancement des Tâches de Calcul (Producteurs - Fan-Out) ---
	for i, calc := range calculators {
		idx := i
		calculator := calc

		// Lancement de la tâche dans le errgroup.
		g.Go(func() error {
			startTime := time.Now()

			// Appel du calcul principal. Les canaux proxy ne sont plus nécessaires.
			res, err := calculator.Calculate(ctx, progressChan, idx, config.N, config.Threshold)

			// Enregistrement du résultat (thread-safe car les indices sont uniques).
			results[idx] = CalculationResult{
				Name:     calculator.Name(),
				Result:   res,
				Duration: time.Since(startTime),
				Err:      err,
			}
			// Retourne l'erreur au errgroup pour propagation.
			return err
		})
	}

	// --- Gestion de l'Affichage (Consommateur) ---
	var displayWg sync.WaitGroup
	displayWg.Add(1)
	go cli.DisplayAggregateProgress(&displayWg, progressChan, len(calculators), out)

	// --- Séquence de Fermeture (Coordination Finale Simplifiée) ---

	// 1. Attendre la fin de toutes les tâches de calcul.
	_ = g.Wait()

	// 2. Fermer le canal central. C'est sûr car tous les producteurs (errgroup) sont terminés.
	close(progressChan)

	// 3. Attendre que l'affichage soit terminé.
	displayWg.Wait()

	return results
}

// analyzeComparisonResults valide l'intégrité des résultats en mode "all".
func analyzeComparisonResults(results []CalculationResult, config AppConfig, out io.Writer) int {
	fmt.Fprintln(out, "\n--- Résultats de la Comparaison (Benchmark & Validation) ---")

	var firstResult *big.Int
	var firstError error
	successCount := 0

	// Tri des résultats par durée pour un affichage intuitif du benchmark.
	sort.Slice(results, func(i, j int) bool {
		// Prioriser les succès et trier par durée.
		if results[i].Err != nil && results[j].Err == nil {
			return false
		}
		if results[i].Err == nil && results[j].Err != nil {
			return true
		}
		return results[i].Duration < results[j].Duration
	})

	for _, res := range results {
		status := "Succès"
		if res.Err != nil {
			status = fmt.Sprintf("Échec (%s)", res.Err.Error())
			if firstError == nil {
				firstError = res.Err
			}
		} else {
			successCount++
			if firstResult == nil {
				firstResult = res.Result
			}
		}
		fmt.Fprintf(out, "  - %-65s | Durée: %-15s | Statut: %s\n", res.Name, res.Duration.String(), status)
	}

	if successCount == 0 {
		fmt.Fprintln(out, "\nStatut Global : Échec. Tous les calculs ont échoué.")
		return handleCalculationError(firstError, 0, config.Timeout, out)
	}

	// Validation de la Cohérence.
	mismatch := false
	for _, res := range results {
		if res.Err == nil && res.Result.Cmp(firstResult) != 0 {
			mismatch = true
			break
		}
	}

	if mismatch {
		fmt.Fprintln(out, "\nStatut Global : Échec Critique ! Les algorithmes ont produit des résultats différents.")
		return ExitErrorMismatch
	}

	fmt.Fprintln(out, "\nStatut Global : Succès. Tous les résultats valides sont identiques.")
	cli.DisplayResult(firstResult, config.N, 0, config.Verbose, out)
	return ExitSuccess
}

// handleCalculationError interprète les erreurs et détermine le code de sortie.
func handleCalculationError(err error, duration time.Duration, timeout time.Duration, out io.Writer) int {
	if err == nil {
		return ExitSuccess
	}

	msg := ""
	if duration > 0 {
		msg = fmt.Sprintf(" après %s", duration)
	}

	// Utilisation de errors.Is pour vérifier les erreurs de contexte.
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(out, "Statut : Échec (Timeout). Le délai imparti (%s) a été dépassé%s.\n", timeout, msg)
		return ExitErrorTimeout
	} else if errors.Is(err, context.Canceled) {
		fmt.Fprintf(out, "Statut : Annulé (Signal reçu ou annulation interne)%s.\n", msg)
		return ExitErrorCanceled
	} else {
		fmt.Fprintf(out, "Statut : Échec. Erreur interne : %v\n", err)
		return ExitErrorGeneric
	}
}
