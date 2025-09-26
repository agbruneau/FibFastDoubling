// EXPLICATION ACADÉMIQUE :
// Le package `main` est le point d'entrée de l'application.
// Ce fichier `main.go` agit comme le "chef d'orchestre" : initialisation (configuration, contexte),
// orchestration des briques logicielles (calculs) et gestion de la fin de vie (codes de sortie, signaux).
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

	// `golang.org/x/sync/errgroup` fournit une synchronisation de goroutines "structurée",
	// simplifiant l'attente de groupe et la propagation des erreurs/annulations.
	"golang.org/x/sync/errgroup"

	// L'importation depuis `internal/` garantit que la logique métier est encapsulée.
	"example.com/fibcalc/internal/cli"
	"example.com/fibcalc/internal/fibonacci"
)

// Constantes pour les codes de sortie standardisés.
const (
	ExitSuccess       = 0
	ExitErrorGeneric  = 1
	ExitErrorTimeout  = 2
	ExitErrorMismatch = 3
	ExitErrorConfig   = 4   // [BONIFICATION] Erreur spécifique pour les problèmes de configuration/validation
	ExitErrorCanceled = 130 // Convention pour SIGINT (Ctrl+C)
)

// Constantes pour la configuration de l'exécution concurrente.
const (
	// ProgressBufferMultiplier détermine la taille du buffer du canal de progression
	// par rapport au nombre de calculateurs. Un buffer évite de bloquer les calculateurs
	// si l'affichage est momentanément lent.
	ProgressBufferMultiplier = 10
)

// AppConfig regroupe tous les paramètres de configuration de l'application.
type AppConfig struct {
	N         uint64
	Verbose   bool
	Timeout   time.Duration
	Algo      string // Sera normalisé en minuscule lors du parsing.
	Threshold int
}

// [BONIFICATION] Validate vérifie la validité sémantique complète de la configuration.
func (c AppConfig) Validate(availableAlgos []string) error {
	if c.Timeout <= 0 {
		return errors.New("le timeout (-timeout) doit être positif")
	}
	if c.Threshold < 0 {
		return fmt.Errorf("le seuil (-threshold) ne peut pas être négatif (valeur : %d)", c.Threshold)
	}

	// Validation de l'algorithme sélectionné.
	if c.Algo != "all" {
		if _, ok := calculatorRegistry[c.Algo]; !ok {
			// Message d'erreur dynamique et précis.
			return fmt.Errorf("algorithme '%s' inconnu. Options valides : 'all' ou [%s]", c.Algo, strings.Join(availableAlgos, ", "))
		}
	}
	return nil
}

// EXPLICATION ACADÉMIQUE : Patron de conception "Registry"
// `calculatorRegistry` associe le nom de l'algorithme à une implémentation concrète.
// Permet un découplage fort et une extensibilité facile (Principe Ouvert/Fermé).
var calculatorRegistry = map[string]fibonacci.Calculator{
	// Le décorateur `NewCalculator` ajoute des fonctionnalités communes (e.g., Lookup Table).
	"fast":   fibonacci.NewCalculator(&fibonacci.OptimizedFastDoubling{}),
	"matrix": fibonacci.NewCalculator(&fibonacci.MatrixExponentiation{}),
}

// init est exécuté automatiquement avant main().
// [BONIFICATION] Utilisé ici pour valider l'intégrité du registry au démarrage (fail-fast).
func init() {
	for name, calc := range calculatorRegistry {
		if calc == nil {
			// panic est approprié ici car c'est une erreur de programmation irrécupérable.
			panic(fmt.Sprintf("Erreur d'initialisation (développement) : le calculateur '%s' est nil dans le registry.", name))
		}
	}
}

// getSortedCalculatorKeys retourne les clés du registry triées pour un affichage déterministe.
func getSortedCalculatorKeys() []string {
	keys := make([]string, 0, len(calculatorRegistry))
	for k := range calculatorRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// main est le point d'entrée du programme. Il gère l'interaction avec l'OS.
func main() {
	// 1. Analyse de la configuration. On utilise os.Stderr pour les erreurs/aide.
	// On passe os.Args[1:] (tous les arguments sauf le nom du programme).
	config, err := parseConfig(os.Args[0], os.Args[1:], os.Stderr)
	if err != nil {
		// Gestion spécifique de la demande d'aide (-h ou --help).
		if errors.Is(err, flag.ErrHelp) {
			// Le message d'aide a déjà été affiché par FlagSet dans parseConfig.
			os.Exit(ExitSuccess)
		}
		// Autres erreurs (parsing invalide ou échec de validation). Le détail a été affiché.
		os.Exit(ExitErrorConfig)
	}

	// 2. Exécution de la logique principale.
	// EXPLICATION ACADÉMIQUE : Séparation de la logique
	// La logique applicative est dans `run`. `main` interagit avec les éléments
	// globaux (OS), tandis que `run` prend ses dépendances (contexte, config, sortie)
	// en arguments, permettant des tests unitaires contrôlés.
	exitCode := run(context.Background(), config, os.Stdout)

	// 3. Terminaison du programme.
	os.Exit(exitCode)
}

// [REFACTORING MAJEUR] : Utilisation de FlagSet pour une meilleure testabilité.
// parseConfig initialise la configuration à partir des arguments de la ligne de commande.
func parseConfig(programName string, args []string, errorWriter io.Writer) (AppConfig, error) {
	// EXPLICATION ACADÉMIQUE : flag.NewFlagSet
	// On crée un FlagSet local au lieu d'utiliser le `flag.CommandLine` global.
	// Cela isole la gestion des arguments et améliore considérablement la testabilité.
	// flag.ContinueOnError permet de gérer les erreurs de parsing nous-mêmes.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(errorWriter)

	// [BONIFICATION] Génération dynamique du message d'aide.
	availableAlgos := getSortedCalculatorKeys()
	algoHelp := fmt.Sprintf("Algorithme : 'all' (comparaison) ou l'un parmi : [%s].", strings.Join(availableAlgos, ", "))

	config := AppConfig{}

	// EXPLICATION ACADÉMIQUE : Fonctions `Var`
	// OPTIMISATION : Utilisation des variantes "Var" (e.g., Uint64Var) pour lier directement
	// les flags aux champs de la structure, évitant les variables pointeurs intermédiaires.
	fs.Uint64Var(&config.N, "n", 100000000, "L'indice 'n' de la séquence de Fibonacci à calculer.")
	fs.BoolVar(&config.Verbose, "v", false, "Affiche le résultat complet (non tronqué).")
	// Ajout d'un alias long standard.
	fs.BoolVar(&config.Verbose, "verbose", false, "Affiche le résultat complet (non tronqué).")
	fs.DurationVar(&config.Timeout, "timeout", 5*time.Minute, "Délai maximum d'exécution (ex: 30s, 1m).")
	fs.StringVar(&config.Algo, "algo", "all", algoHelp)
	fs.IntVar(&config.Threshold, "threshold", fibonacci.DefaultParallelThreshold, "Seuil (en bits) pour activer la multiplication parallèle (Karatsuba).")

	// Analyse des arguments.
	if err := fs.Parse(args); err != nil {
		// Retourne l'erreur (peut être flag.ErrHelp).
		return AppConfig{}, err
	}

	// Normalisation des entrées.
	config.Algo = strings.ToLower(config.Algo)

	// Validation sémantique centralisée.
	if err := config.Validate(availableAlgos); err != nil {
		// Affiche l'erreur spécifique et l'usage standard.
		fmt.Fprintln(errorWriter, "Erreur de configuration :", err)
		fs.Usage()
		// On retourne une erreur générique pour signaler l'échec à main().
		return AppConfig{}, errors.New("configuration invalide")
	}

	return config, nil
}

// CalculationResult stocke le résultat d'un seul calcul et ses métadonnées.
type CalculationResult struct {
	Name     string
	Result   *big.Int
	Duration time.Duration
	Err      error
}

// run contient la logique principale de l'application. Elle est conçue pour être testable.
func run(ctx context.Context, config AppConfig, out io.Writer) int {
	// --- GESTION ROBUSTE DU CONTEXTE (Timeout & Signaux) ---
	// EXPLICATION ACADÉMIQUE : Gestion des ressources et Annulation
	// Le `context` est utilisé pour propager l'annulation à travers l'application.

	// 1. Timeout Global : Crée un contexte qui s'annule automatiquement après le délai.
	ctx, cancelTimeout := context.WithTimeout(ctx, config.Timeout)
	// `defer` garantit la libération des ressources associées au timer (essentiel pour éviter les fuites).
	defer cancelTimeout()

	// 2. Gestion des Signaux OS (Graceful Shutdown) :
	// Crée un contexte qui s'annule si SIGINT (Ctrl+C) ou SIGTERM est reçu.
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// --- INITIALISATION & CONFIGURATION ---
	fmt.Fprintln(out, "--- Configuration ---")
	fmt.Fprintf(out, "Calcul de F(%d).\n", config.N)
	fmt.Fprintf(out, "Système : CPU Cores=%d | Go Runtime=%s\n", runtime.NumCPU(), runtime.Version())
	fmt.Fprintf(out, "Paramètres : Timeout=%s | Parallel Threshold=%d bits\n", config.Timeout, config.Threshold)

	// --- SÉLECTION DES CALCULATEURS ---
	// [REFACTORING] Logique extraite dans une fonction dédiée.
	// Puisque la configuration est déjà validée, cette étape ne peut pas échouer.
	calculatorsToRun := getCalculatorsToRun(config)

	// Affichage du mode d'exécution.
	if len(calculatorsToRun) > 1 {
		fmt.Fprintln(out, "Mode : Comparaison (Exécution parallèle).")
	} else {
		// On sait qu'il y a au moins un calculateur car le registre n'est pas vide (vérifié par init) et la config est valide.
		fmt.Fprintln(out, "Mode : Simple exécution.")
		fmt.Fprintf(out, "Algorithme : %s\n", calculatorsToRun[0].Name())
	}

	fmt.Fprintln(out, "\n--- Exécution ---")

	// --- EXÉCUTION ET ANALYSE ---
	results := executeCalculations(ctx, calculatorsToRun, config, out)

	// Cas 1 : Un seul calcul demandé.
	if len(results) == 1 {
		res := results[0]
		fmt.Fprintln(out, "\n--- Résultat Final ---")
		if res.Err != nil {
			return handleCalculationError(res.Err, res.Duration, config.Timeout, out)
		}
		cli.DisplayResult(res.Result, config.N, res.Duration, config.Verbose, out)
		return ExitSuccess
	}

	// Cas 2 : Comparaison (mode "all").
	return analyzeComparisonResults(results, config, out)
}

// getCalculatorsToRun sélectionne les calculateurs en fonction de la configuration validée.
func getCalculatorsToRun(config AppConfig) []fibonacci.Calculator {
	// config.Algo est déjà validé et normalisé (minuscule).

	if config.Algo == "all" {
		// Mode "all" : On utilise les clés triées pour garantir un ordre déterministe.
		keys := getSortedCalculatorKeys()
		calculators := make([]fibonacci.Calculator, len(keys))
		for i, k := range keys {
			calculators[i] = calculatorRegistry[k]
		}
		return calculators
	}

	// Mode simple : On sait que l'algorithme existe car il a été validé par AppConfig.Validate().
	return []fibonacci.Calculator{calculatorRegistry[config.Algo]}
}

// executeCalculations orchestre l'exécution parallèle des calculs (Concurrence Structurée).
func executeCalculations(ctx context.Context, calculators []fibonacci.Calculator, config AppConfig, out io.Writer) []CalculationResult {
	// EXPLICATION ACADÉMIQUE : `errgroup.WithContext`
	// Crée un groupe de goroutines et un contexte dérivé.
	g, ctx := errgroup.WithContext(ctx)

	results := make([]CalculationResult, len(calculators))
	// Canal "buffered" pour agréger les mises à jour de progression sans bloquer les workers.
	progressChan := make(chan fibonacci.ProgressUpdate, len(calculators)*ProgressBufferMultiplier)

	// --- PATRON FAN-OUT : Lancement des workers de calcul (Producteurs) ---
	for i, calc := range calculators {
		// EXPLICATION ACADÉMIQUE : Capture des variables de boucle
		// CRUCIAL (avant Go 1.22) : Créer des copies locales (`idx`, `calculator`) avant
		// de les utiliser dans la goroutine (closure). Sinon, toutes les goroutines
		// utiliseraient la dernière valeur de la boucle (bug classique de concurrence).
		idx := i
		calculator := calc

		// Lancement de la tâche dans une goroutine gérée par le groupe.
		g.Go(func() error {
			startTime := time.Now()

			// Exécution du calcul (appel bloquant). Le contexte permet l'annulation interne.
			res, err := calculator.Calculate(ctx, progressChan, idx, config.N, config.Threshold)

			// Stockage du résultat. C'est "thread-safe" car chaque goroutine écrit à un index unique.
			results[idx] = CalculationResult{
				Name:     calculator.Name(),
				Result:   res,
				Duration: time.Since(startTime),
				Err:      err,
			}

			// DÉCISION DE CONCEPTION (Benchmark) :
			// On retourne toujours `nil` pour empêcher `errgroup` d'annuler le contexte
			// si un calcul échoue ("fail-fast"). Dans un benchmark, on veut que tous les
			// algorithmes tentent de terminer (sauf timeout global). L'erreur est stockée
			// dans `results[idx]` pour analyse ultérieure.
			return nil
		})
	}

	// --- GESTION DE L'AFFICHAGE (Consommateur) ---
	var displayWg sync.WaitGroup
	displayWg.Add(1)
	go cli.DisplayAggregateProgress(&displayWg, progressChan, len(calculators), out)

	// --- SÉQUENCE DE FERMETURE (Coordination / FAN-IN) ---

	// 1. Attendre la fin de tous les producteurs (calculateurs).
	// `g.Wait()` bloque jusqu'à la fin de toutes les goroutines du groupe.
	_ = g.Wait()

	// 2. Fermer le canal central.
	// Signale au consommateur qu'il n'y aura plus de messages.
	// C'est sûr car tous les producteurs ont terminé (grâce à g.Wait()).
	close(progressChan)

	// 3. Attendre la fin du consommateur (affichage).
	// Assure que tous les messages restants dans le canal ont été traités.
	displayWg.Wait()

	return results
}

// analyzeComparisonResults valide l'intégrité et compare les performances en mode "all".
func analyzeComparisonResults(results []CalculationResult, config AppConfig, out io.Writer) int {
	fmt.Fprintln(out, "\n--- Résultats de la Comparaison (Benchmark & Validation) ---")

	// Tri des résultats pour un affichage intuitif.
	sort.Slice(results, func(i, j int) bool {
		resI := results[i]
		resJ := results[j]

		// Règle 1 : Les succès apparaissent avant les échecs.
		if resI.Err == nil && resJ.Err != nil {
			return true
		}
		if resI.Err != nil && resJ.Err == nil {
			return false
		}

		// Règle 2 : Si les statuts sont identiques, on trie par durée (du plus rapide au plus lent).
		return resI.Duration < resJ.Duration
	})

	var firstValidResult *big.Int
	var firstError error
	successCount := 0

	// Affichage des résultats triés et collecte des informations de validation.
	for _, res := range results {
		status := "Succès"
		if res.Err != nil {
			status = fmt.Sprintf("Échec (%s)", res.Err.Error())
			if firstError == nil {
				firstError = res.Err
			}
		} else {
			successCount++
			if firstValidResult == nil {
				firstValidResult = res.Result
			}
		}
		fmt.Fprintf(out, "  - %-65s | Durée: %-15s | Statut: %s\n", res.Name, res.Duration.String(), status)
	}

	// Analyse globale
	if successCount == 0 {
		fmt.Fprintln(out, "\nStatut Global : Échec. Tous les calculs ont échoué.")
		// On utilise la première erreur rencontrée pour déterminer le code de sortie.
		return handleCalculationError(firstError, 0, config.Timeout, out)
	}

	// Validation de la cohérence (Test d'intégrité) :
	// On vérifie que tous les algorithmes qui ont réussi ont produit le MÊME résultat.
	mismatch := false
	for _, res := range results {
		// `big.Int.Cmp` est la méthode correcte pour comparer des `big.Int`.
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
	// Affiche le résultat final (ils sont tous identiques). La durée (0) n'est pas pertinente ici.
	cli.DisplayResult(firstValidResult, config.N, 0, config.Verbose, out)
	return ExitSuccess
}

// handleCalculationError interprète les erreurs et détermine le code de sortie approprié.
func handleCalculationError(err error, duration time.Duration, timeout time.Duration, out io.Writer) int {
	if err == nil {
		return ExitSuccess
	}

	msgSuffix := ""
	if duration > 0 {
		msgSuffix = fmt.Sprintf(" après %s", duration)
	}

	// EXPLICATION ACADÉMIQUE : `errors.Is`
	// Manière idiomatique et robuste de vérifier les erreurs en Go. Elle permet de
	// vérifier si une erreur dans une chaîne d'erreurs (error wrapping) correspond
	// à une valeur spécifique (comme `context.DeadlineExceeded`).
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(out, "Statut : Échec (Timeout). Le délai imparti (%s) a été dépassé%s.\n", timeout, msgSuffix)
		return ExitErrorTimeout
	} else if errors.Is(err, context.Canceled) {
		// Cela couvre généralement l'annulation par signal (Ctrl+C) ou une annulation interne.
		fmt.Fprintf(out, "Statut : Annulé (Signal reçu ou annulation interne)%s.\n", msgSuffix)
		return ExitErrorCanceled
	} else {
		fmt.Fprintf(out, "Statut : Échec. Erreur interne inattendue : %v\n", err)
		return ExitErrorGeneric
	}
}
