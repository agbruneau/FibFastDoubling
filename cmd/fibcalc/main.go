//
// MODULE ACADÉMIQUE : POINT D'ENTRÉE ET ORCHESTRATION (COMPOSITION ROOT)
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier est le "Composition Root" de l'application. C'est ici que toutes les
// dépendances et les modules sont assemblés ("wired together"). Il illustre des concepts
// fondamentaux de l'ingénierie logicielle robuste en Go :
//  1. SÉPARATION DES PRÉOCCUPATIONS : La fonction `main` est minimale et traite avec
//     les "impuretés" du monde extérieur (OS, arguments, signaux). La logique applicative
//     pure et testable est déléguée à la fonction `run`.
//  2. GESTION DU CYCLE DE VIE : Orchestration complète du cycle de vie de l'application,
//     de l'initialisation (configuration, contexte) à la terminaison (gestion des erreurs,
//     arrêt propre, codes de sortie).
//  3. CONCURRENCE STRUCTURÉE : Utilisation de `errgroup` pour gérer des groupes de
//     goroutines, garantissant qu'aucune ne soit "orpheline" et que les erreurs
//     soient propagées et gérées correctement.
//  4. GESTION DES SIGNAUX (GRACEFUL SHUTDOWN) : Intégration idiomatique du `context`
//     de Go pour répondre aux signaux de l'OS (ex: Ctrl+C) et aux timeouts.
//  5. INJECTION DE DÉPENDANCES : La fonction `run` reçoit ses dépendances (contexte,
//     configuration, writer), ce qui la rend indépendante de l'état global et facile à tester.
//
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

	// EXPLICATION ACADÉMIQUE : `golang.org/x/sync/errgroup`
	// Fournit une "concurrence structurée". Contrairement à un `sync.WaitGroup` simple,
	// un `errgroup` lie un groupe de goroutines à un `context`. Si une goroutine retourne
	// une erreur ou si le contexte est annulé, le contexte du groupe est annulé,
	// signalant à toutes les autres goroutines du groupe de s'arrêter.
	"golang.org/x/sync/errgroup"

	"example.com/fibcalc/internal/cli"
	"example.com/fibcalc/internal/fibonacci"
)

// EXPLICATION ACADÉMIQUE : Codes de Sortie (Exit Codes)
// L'utilisation de codes de sortie standardisés est une bonne pratique pour les CLI,
// car elle permet à d'autres programmes (scripts shell, CI/CD) de réagir de manière
// appropriée au résultat de l'exécution.
const (
	ExitSuccess       = 0   // Opération réussie.
	ExitErrorGeneric  = 1   // Erreur générique non spécifiée.
	ExitErrorTimeout  = 2   // L'opération a dépassé le temps imparti.
	ExitErrorMismatch = 3   // En mode comparaison, les résultats ne correspondent pas.
	ExitErrorConfig   = 4   // Erreur dans les arguments ou la configuration.
	ExitErrorCanceled = 130 // Convention pour une terminaison suite à un signal (SIGINT/Ctrl+C).
)

const (
	// La taille du buffer du canal de progression est un multiple du nombre de calculateurs.
	// Un buffer permet aux producteurs (calculateurs) de ne pas bloquer si le consommateur (UI)
	// est momentanément lent, améliorant le découplage et la performance.
	ProgressBufferMultiplier = 10
)

// AppConfig agrège tous les paramètres de configuration de l'application.
// C'est une bonne pratique de regrouper la configuration dans une structure dédiée.
type AppConfig struct {
	N         uint64
	Verbose   bool
	Timeout   time.Duration
	Algo      string
	Threshold int
}

// Validate vérifie la validité sémantique de la configuration.
// Exécuter la validation après le parsing permet de s'assurer que les combinaisons
// de paramètres sont logiques (principe du "Fail-Fast").
func (c AppConfig) Validate(availableAlgos []string) error {
	if c.Timeout <= 0 {
		return errors.New("le timeout (-timeout) doit être positif")
	}
	if c.Threshold < 0 {
		return fmt.Errorf("le seuil (-threshold) ne peut pas être négatif (valeur : %d)", c.Threshold)
	}
	if c.Algo != "all" {
		if _, ok := calculatorRegistry[c.Algo]; !ok {
			return fmt.Errorf("algorithme '%s' inconnu. Options valides : 'all' ou [%s]", c.Algo, strings.Join(availableAlgos, ", "))
		}
	}
	return nil
}

// EXPLICATION ACADÉMIQUE : Patron de Conception "Registry"
// Le `calculatorRegistry` est une carte qui associe un identifiant (string) à une
// implémentation concrète de l'interface `fibonacci.Calculator`. Ce patron permet
// un couplage faible et respecte le Principe Ouvert/Fermé (SOLID) : on peut ajouter
// de nouveaux algorithmes (ouvert à l'extension) sans modifier le code qui l'utilise
// (fermé à la modification).
var calculatorRegistry = map[string]fibonacci.Calculator{
	"fast":   fibonacci.NewCalculator(&fibonacci.OptimizedFastDoubling{}),
	"matrix": fibonacci.NewCalculator(&fibonacci.MatrixExponentiation{}),
}

// EXPLICATION ACADÉMIQUE : La fonction `init`
// `init` est exécutée par le runtime Go avant `main`. Elle est utile pour des validations
// ou initialisations qui doivent avoir lieu une seule fois au démarrage. Ici, on l'utilise
// pour une "vérification de santé" (sanity check) : s'assurer qu'un développeur n'a pas
// accidentellement enregistré un calculateur `nil`, ce qui causerait un "panic" plus tard.
func init() {
	for name, calc := range calculatorRegistry {
		if calc == nil {
			panic(fmt.Sprintf("Erreur d'initialisation (développement) : le calculateur '%s' est nil dans le registry.", name))
		}
	}
}

// getSortedCalculatorKeys retourne les clés du registre triées.
// C'est essentiel pour garantir un affichage déterministe et cohérent à chaque exécution.
func getSortedCalculatorKeys() []string {
	keys := make([]string, 0, len(calculatorRegistry))
	for k := range calculatorRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// main est le point d'entrée. Son rôle est de gérer les interactions avec l'OS
// et d'orchestrer les composants de haut niveau.
func main() {
	// Étape 1 : Analyser la configuration depuis les arguments de la ligne de commande.
	config, err := parseConfig(os.Args[0], os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(ExitSuccess)
		}
		os.Exit(ExitErrorConfig)
	}

	// Étape 2 : Démarrer la logique applicative principale.
	// `context.Background()` fournit le contexte racine, qui n'est jamais annulé.
	exitCode := run(context.Background(), config, os.Stdout)

	// Étape 3 : Terminer le programme avec le code de sortie approprié.
	os.Exit(exitCode)
}

// parseConfig analyse les arguments, valide la configuration et la retourne.
// EXPLICATION ACADÉMIQUE : Testabilité du Parsing d'Arguments
// En créant un `flag.NewFlagSet` local au lieu d'utiliser le `flag` global, et en
// acceptant les arguments (`args`) et le writer d'erreurs (`errorWriter`) comme
// paramètres, cette fonction devient pure et facilement testable unitairement,
// sans dépendre de l'état global du programme ou de `os.Args`.
func parseConfig(programName string, args []string, errorWriter io.Writer) (AppConfig, error) {
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(errorWriter)

	availableAlgos := getSortedCalculatorKeys()
	algoHelp := fmt.Sprintf("Algorithme : 'all' (comparaison) ou l'un parmi : [%s].", strings.Join(availableAlgos, ", "))

	config := AppConfig{}
	fs.Uint64Var(&config.N, "n", 100000000, "L'indice 'n' de la séquence de Fibonacci à calculer.")
	fs.BoolVar(&config.Verbose, "v", false, "Affiche le résultat complet (non tronqué).")
	fs.BoolVar(&config.Verbose, "verbose", false, "Affiche le résultat complet (non tronqué).")
	fs.DurationVar(&config.Timeout, "timeout", 5*time.Minute, "Délai maximum d'exécution (ex: 30s, 1m).")
	fs.StringVar(&config.Algo, "algo", "all", algoHelp)
	fs.IntVar(&config.Threshold, "threshold", fibonacci.DefaultParallelThreshold, "Seuil (en bits) pour activer la multiplication parallèle.")

	if err := fs.Parse(args); err != nil {
		return AppConfig{}, err
	}

	config.Algo = strings.ToLower(config.Algo)

	if err := config.Validate(availableAlgos); err != nil {
		fmt.Fprintln(errorWriter, "Erreur de configuration :", err)
		fs.Usage()
		return AppConfig{}, errors.New("configuration invalide")
	}

	return config, nil
}

// CalculationResult stocke le résultat d'un calcul et ses métadonnées.
type CalculationResult struct {
	Name     string
	Result   *big.Int
	Duration time.Duration
	Err      error
}

// run contient la logique principale de l'application. Elle est testable.
func run(ctx context.Context, config AppConfig, out io.Writer) int {
	// --- GESTION DU CONTEXTE ET DE L'ANNULATION ---
	// EXPLICATION ACADÉMIQUE : Composition des Contextes pour un Arrêt Propre (Graceful Shutdown)
	// Le `context` de Go est un mécanisme puissant pour propager des signaux d'annulation.
	// Ici, nous composons deux types d'annulation :
	// 1. Basée sur le temps (`context.WithTimeout`) : Annule si le calcul dure trop longtemps.
	// 2. Basée sur un signal OS (`signal.NotifyContext`) : Annule si l'utilisateur appuie sur Ctrl+C.
	// Le contexte `ctx` passé aux fonctions en aval sera annulé si L'UN OU L'AUTRE de ces événements se produit.
	// Les `defer cancel()` sont cruciaux pour libérer les ressources associées aux contextes.
	ctx, cancelTimeout := context.WithTimeout(ctx, config.Timeout)
	defer cancelTimeout()
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	fmt.Fprintln(out, "--- Configuration ---")
	fmt.Fprintf(out, "Calcul de F(%d).\n", config.N)
	fmt.Fprintf(out, "Système : CPU Cores=%d | Go Runtime=%s\n", runtime.NumCPU(), runtime.Version())
	fmt.Fprintf(out, "Paramètres : Timeout=%s | Parallel Threshold=%d bits\n", config.Timeout, config.Threshold)

	calculatorsToRun := getCalculatorsToRun(config)
	if len(calculatorsToRun) > 1 {
		fmt.Fprintln(out, "Mode : Comparaison (Exécution parallèle).")
	} else {
		fmt.Fprintf(out, "Mode : Simple exécution. Algorithme : %s\n", calculatorsToRun[0].Name())
	}
	fmt.Fprintln(out, "\n--- Exécution ---")

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

// getCalculatorsToRun sélectionne les calculateurs à exécuter.
func getCalculatorsToRun(config AppConfig) []fibonacci.Calculator {
	if config.Algo == "all" {
		keys := getSortedCalculatorKeys()
		calculators := make([]fibonacci.Calculator, len(keys))
		for i, k := range keys {
			calculators[i] = calculatorRegistry[k]
		}
		return calculators
	}
	return []fibonacci.Calculator{calculatorRegistry[config.Algo]}
}

// executeCalculations orchestre l'exécution concurrente des calculs.
// EXPLICATION ACADÉMIQUE : Patron de Concurrence "Fan-Out / Fan-In"
func executeCalculations(ctx context.Context, calculators []fibonacci.Calculator, config AppConfig, out io.Writer) []CalculationResult {
	g, ctx := errgroup.WithContext(ctx)
	results := make([]CalculationResult, len(calculators))
	progressChan := make(chan fibonacci.ProgressUpdate, len(calculators)*ProgressBufferMultiplier)

	// --- Étape 1 : FAN-OUT (Distribution) ---
	// On lance une goroutine ("worker") pour chaque calcul à effectuer.
	for i, calc := range calculators {
		// EXPLICATION ACADÉMIQUE : Capture de Variable de Boucle
		// Avant Go 1.22, les variables de boucle `i` et `calc` étaient partagées par toutes
		// les itérations. Sans ces copies locales (`idx`, `calculator`), toutes les goroutines
		// auraient capturé les valeurs de la DERNIÈRE itération, un bug de concurrence classique.
		idx, calculator := i, calc
		g.Go(func() error {
			startTime := time.Now()
			res, err := calculator.Calculate(ctx, progressChan, idx, config.N, config.Threshold)

			// Écriture "Thread-Safe" : Chaque goroutine écrit dans un index unique du slice `results`,
			// il n'y a donc pas de conflit d'accès ("race condition") et pas besoin de mutex.
			results[idx] = CalculationResult{
				Name: calculator.Name(), Result: res, Duration: time.Since(startTime), Err: err,
			}

			// DÉCISION DE CONCEPTION : Dans un benchmark, un échec ne doit pas tout arrêter.
			// On retourne `nil` pour que `errgroup` ne déclenche pas l'annulation du contexte
			// si un seul calcul échoue. L'erreur est gérée plus tard.
			return nil
		})
	}

	// --- Consommateur d'UI (lancé en parallèle) ---
	var displayWg sync.WaitGroup
	displayWg.Add(1)
	go cli.DisplayAggregateProgress(&displayWg, progressChan, len(calculators), out)

	// --- Étape 2 : FAN-IN (Synchronisation et Collecte) ---
	// SÉQUENCE D'ARRÊT CRITIQUE :
	// 1. Attendre la fin de tous les producteurs (calculateurs).
	_ = g.Wait()
	// 2. Fermer le canal de communication. C'est sûr car plus aucun producteur n'écrit.
	//    La fermeture signale au consommateur (UI) qu'il n'y aura plus de messages.
	close(progressChan)
	// 3. Attendre la fin du consommateur. Cela garantit que l'UI a bien traité tous
	//    les messages restants et a fini son affichage avant de continuer.
	displayWg.Wait()

	return results
}

// analyzeComparisonResults analyse les résultats en mode "all".
func analyzeComparisonResults(results []CalculationResult, config AppConfig, out io.Writer) int {
	sort.Slice(results, func(i, j int) bool {
		// Tri complexe pour un affichage lisible : succès d'abord, puis par durée.
		if (results[i].Err == nil) != (results[j].Err == nil) {
			return results[i].Err == nil
		}
		return results[i].Duration < results[j].Duration
	})

	var firstValidResult *big.Int
	var firstError error
	successCount := 0

	// --- Préparation pour un affichage tabulaire amélioré ---
	// Initialiser les largeurs avec les en-têtes de colonnes comme base
	col1Width := len("Algorithme")
	col2Width := len("Durée")

	// Structure pour stocker les données formatées avant l'affichage
	type displayRow struct {
		Name     string
		Duration string
		Status   string
	}
	displayData := make([]displayRow, len(results))

	// Première passe : formater les données et calculer les largeurs de colonnes
	for i, res := range results {
		var status string
		if res.Err != nil {
			status = fmt.Sprintf("❌ Échec (%v)", res.Err)
			if firstError == nil {
				firstError = res.Err
			}
		} else {
			status = "✅ Succès"
			successCount++
			if firstValidResult == nil {
				firstValidResult = res.Result
			}
		}

		displayData[i] = displayRow{
			Name:     res.Name,
			Duration: res.Duration.String(),
			Status:   status,
		}

		// Mettre à jour les largeurs maximales
		if len(displayData[i].Name) > col1Width {
			col1Width = len(displayData[i].Name)
		}
		if len(displayData[i].Duration) > col2Width {
			col2Width = len(displayData[i].Duration)
		}
	}

	// --- Affichage des résultats ---
	fmt.Fprintln(out, "\n--- Résultats de la Comparaison (Benchmark & Validation) ---")

	// Définir le format des lignes avec des largeurs dynamiques et un padding
	rowFormat := fmt.Sprintf("  %%-%ds │ %%-%ds │ %%s\n", col1Width, col2Width)

	// Afficher l'en-tête du tableau, avec un espacement vertical
	fmt.Fprintf(out, "\n"+rowFormat, "Algorithme", "Durée", "Statut")

	// Afficher la ligne de séparation
	separator := fmt.Sprintf("  %s┼%s┼%s",
		strings.Repeat("─", col1Width+1),
		strings.Repeat("─", col2Width+2),
		strings.Repeat("─", 45), // Ligne assez longue pour la colonne statut
	)
	fmt.Fprintln(out, separator)

	// Afficher les lignes de données
	for _, data := range displayData {
		fmt.Fprintf(out, rowFormat, data.Name, data.Duration, data.Status)
	}

	if successCount == 0 {
		fmt.Fprintln(out, "\nStatut Global : Échec. Tous les calculs ont échoué.")
		return handleCalculationError(firstError, 0, config.Timeout, out)
	}

	// Validation croisée : tous les succès doivent donner le même résultat.
	mismatch := false
	for _, res := range results {
		if res.Err == nil && res.Result.Cmp(firstValidResult) != 0 {
			mismatch = true
			break
		}
	}
	if mismatch {
		fmt.Fprintln(out, "\nStatut Global : Échec Critique ! Incohérence détectée (les algorithmes produisent des résultats différents).")
		return ExitErrorMismatch
	}

	fmt.Fprintln(out, "\nStatut Global : Succès. Tous les résultats valides sont identiques.")
	cli.DisplayResult(firstValidResult, config.N, 0, config.Verbose, out)
	return ExitSuccess
}

// handleCalculationError interprète une erreur et retourne le code de sortie approprié.
func handleCalculationError(err error, duration time.Duration, timeout time.Duration, out io.Writer) int {
	if err == nil {
		return ExitSuccess
	}
	msgSuffix := ""
	if duration > 0 {
		msgSuffix = fmt.Sprintf(" après %s", duration)
	}

	// EXPLICATION ACADÉMIQUE : `errors.Is` vs `==`
	// Utiliser `errors.Is(err, context.DeadlineExceeded)` est plus robuste que `err == context.DeadlineExceeded`.
	// `errors.Is` peut "déballer" (`unwrap`) une chaîne d'erreurs pour voir si l'erreur
	// recherchée s'y trouve. C'est essentiel car une bibliothèque peut retourner une erreur
	// qui "enveloppe" l'erreur de contexte originale (ex: `fmt.Errorf("le calcul a échoué: %w", ctx.Err())`).
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(out, "Statut : Échec (Timeout). Le délai imparti (%s) a été dépassé%s.\n", timeout, msgSuffix)
		return ExitErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintf(out, "Statut : Annulé (Signal reçu ou annulation interne)%s.\n", msgSuffix)
		return ExitErrorCanceled
	}
	fmt.Fprintf(out, "Statut : Échec. Erreur interne inattendue : %v\n", err)
	return ExitErrorGeneric
}