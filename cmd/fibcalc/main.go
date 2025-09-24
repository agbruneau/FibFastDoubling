// EXPLICATION ACADÉMIQUE :
// Le package `main` est le point d'entrée de toute application exécutable en Go.
// Ce fichier `main.go` sert de "chef d'orchestre" : il ne contient pas la logique métier
// principale (qui est dans `internal/`), mais il est responsable de :
//  1. L'initialisation de l'application (lecture des arguments de la ligne de commande).
//  2. La configuration de l'environnement d'exécution (contexte, timeouts, signaux).
//  3. L'orchestration des différentes briques logicielles (lancer les calculs, afficher les résultats).
//  4. La gestion de la fin de vie de l'application (codes de sortie).
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
	// C'est une bibliothèque externe (mais maintenue par l'équipe Go) qui fournit une
	// synchronisation de goroutines "structurée". Un `errgroup` simplifie deux tâches courantes :
	//  1. Attendre la fin d'un groupe de goroutines (similaire à `sync.WaitGroup`).
	//  2. Propager la première erreur qui survient dans n'importe laquelle des goroutines
	//     et annuler le contexte pour toutes les autres, garantissant un "fail-fast".
	// C'est un outil fondamental pour la programmation concurrente robuste en Go.
	"golang.org/x/sync/errgroup"

	// EXPLICATION ACADÉMIQUE : Structure de projet Go
	// L'importation de paquets depuis `internal/` est une convention forte en Go.
	// Le compilateur Go interdit à tout code externe à ce projet d'importer des paquets
	// qui se trouvent dans un répertoire `internal/`. Cela garantit que la logique
	// métier principale de votre application ne peut pas être utilisée de manière
	// inattendue par d'autres projets, renforçant l'encapsulation.
	"example.com/fibcalc/internal/cli"
	"example.com/fibcalc/internal/fibonacci"
)

// EXPLICATION ACADÉMIQUE : Constantes pour les codes de sortie
// Définir des constantes pour les codes de sortie est une bonne pratique.
// Cela rend le code plus lisible (e.g., `return ExitErrorTimeout` est plus clair que `return 2`)
// et facilite la maintenance. Les codes de sortie sont une convention Unix pour communiquer
// le résultat d'un programme à son environnement (e.g., un script shell).
// 0 = succès, >0 = erreur. Le code 130 est souvent utilisé pour une interruption par l'utilisateur (Ctrl+C).
const (
	ExitSuccess       = 0
	ExitErrorGeneric  = 1
	ExitErrorTimeout  = 2
	ExitErrorCanceled = 130 // Convention pour SIGINT (Ctrl+C)
	ExitErrorMismatch = 3
)

// AppConfig est une structure qui regroupe tous les paramètres de configuration
// de l'application. C'est une bonne pratique pour éviter de passer de nombreux
// arguments individuels à travers les fonctions.
type AppConfig struct {
	N         uint64
	Verbose   bool
	Timeout   time.Duration
	Algo      string
	Threshold int
}

// EXPLICATION ACADÉMIQUE : Patron de conception "Registry"
// `calculatorRegistry` est une implémentation simple du patron "Registry".
// C'est une map qui associe une chaîne de caractères (le nom de l'algorithme) à une
// implémentation concrète de l'interface `fibonacci.Calculator`.
// Avantages :
//  - Découplage : Le code principal n'a pas besoin de connaître les détails de chaque algo.
//  - Extensibilité : Pour ajouter un nouvel algorithme, il suffit de l'ajouter à cette map.
//    Aucune autre partie du code n'a besoin d'être modifiée.
var calculatorRegistry = map[string]fibonacci.Calculator{
	// Le décorateur `NewCalculator` encapsule chaque implémentation de base
	// pour y ajouter des fonctionnalités communes (comme la Lookup Table).
	"fast":   fibonacci.NewCalculator(&fibonacci.OptimizedFastDoubling{}),
	"matrix": fibonacci.NewCalculator(&fibonacci.MatrixExponentiation{}),
}

// main est la fonction principale, le point d'entrée du programme.
func main() {
	// EXPLICATION ACADÉMIQUE : Le paquet `flag`
	// Le paquet `flag` est la manière standard en Go de parser les arguments de la
	// ligne de commande.
	// - `flag.Uint64`, `flag.Bool`, etc., définissent les flags attendus, leur valeur
	//   par défaut et leur description (qui est utilisée pour générer le message d'aide avec `-h`).
	// - Les fonctions retournent des pointeurs vers les valeurs.
	nFlag := flag.Uint64("n", 100000000, "L'indice 'n' de la séquence de Fibonacci à calculer.")
	verboseFlag := flag.Bool("v", false, "Affiche le résultat complet.")
	timeoutFlag := flag.Duration("timeout", 5*time.Minute, "Délai maximum (ex: 30s, 1m).")
	algoFlag := flag.String("algo", "all", "Algorithme : 'fast', 'matrix', ou 'all'.")
	thresholdFlag := flag.Int("threshold", fibonacci.DefaultParallelThreshold, "Seuil (en bits) pour activer la multiplication parallèle.")

	// `flag.Parse()` analyse les arguments de la ligne de commande et remplit les variables
	// pointées par les pointeurs retournés ci-dessus.
	flag.Parse()

	// Les valeurs des flags (obtenues via déréférencement `*`) sont utilisées pour
	// peupler la structure de configuration `AppConfig`.
	config := AppConfig{
		N:         *nFlag,
		Verbose:   *verboseFlag,
		Timeout:   *timeoutFlag,
		Algo:      *algoFlag,
		Threshold: *thresholdFlag,
	}

	// EXPLICATION ACADÉMIQUE : Séparation de la logique
	// La logique de l'application est dans la fonction `run`, pas directement dans `main`.
	// C'est une pratique essentielle pour la testabilité. `main` interagit avec des
	// éléments globaux (ligne de commande, `os.Exit`), ce qui la rend difficile à tester.
	// `run` prend ses dépendances (`context`, `config`, `io.Writer`) comme arguments,
	// ce qui permet de la tester unitairement en lui passant des objets "mock" ou contrôlés.
	exitCode := run(context.Background(), config, os.Stdout)
	os.Exit(exitCode)
}

// CalculationResult stocke le résultat d'un seul calcul, y compris les métadonnées
// comme la durée et l'erreur éventuelle.
type CalculationResult struct {
	Name     string
	Result   *big.Int
	Duration time.Duration
	Err      error
}

// run contient la logique principale de l'application. Elle est conçue pour être testable.
func run(ctx context.Context, config AppConfig, out io.Writer) int {
	// --- GESTION ROBUSTE DU CONTEXTE ---
	// EXPLICATION ACADÉMIQUE : Le `context.Context`
	// Le `context` est un standard en Go pour gérer l'annulation, les timeouts et la
	// propagation de valeurs à travers les appels de fonctions, en particulier dans
	// les systèmes concurrents.

	// 1. Décoration avec Timeout :
	// `context.WithTimeout` crée un nouveau contexte qui sera automatiquement annulé
	// lorsque le `config.Timeout` sera écoulé.
	ctx, cancelTimeout := context.WithTimeout(ctx, config.Timeout)
	// `defer cancelTimeout()` est crucial. Il garantit que les ressources associées
	// au timeout sont libérées à la fin de la fonction, même si le timeout n'est
	// pas atteint. Oublier ce `defer` est une source courante de fuites de mémoire.
	defer cancelTimeout()

	// 2. Décoration avec Signaux OS :
	// `signal.NotifyContext` crée un autre contexte qui sera annulé si le programme
	// reçoit l'un des signaux spécifiés (ici, SIGINT pour Ctrl+C ou SIGTERM pour
	// une demande d'arrêt standard). C'est la manière moderne et idiomatique de
	// gérer un "graceful shutdown".
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// Affichage de la configuration initiale.
	fmt.Fprintf(out, "--- Configuration ---\n")
	fmt.Fprintf(out, "Calcul de F(%d).\n", config.N)
	fmt.Fprintf(out, "Système : CPU Cores=%d | Go Runtime=%s\n", runtime.NumCPU(), runtime.Version())
	fmt.Fprintf(out, "Paramètres : Timeout=%s | Parallel Threshold=%d bits\n", config.Timeout, config.Threshold)

	// --- SÉLECTION DES CALCULATEURS ---
	var calculatorsToRun []fibonacci.Calculator
	algo := strings.ToLower(config.Algo)

	if algo == "all" {
		// En mode "all", on exécute tous les algorithmes enregistrés pour les comparer.
		fmt.Fprintf(out, "Mode : Comparaison (Exécution parallèle).\n")
		// Il est important de trier les clés pour garantir un ordre d'exécution déterministe,
		// ce qui rend le comportement du programme plus prévisible et reproductible.
		keys := make([]string, 0, len(calculatorRegistry))
		for k := range calculatorRegistry {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			calculatorsToRun = append(calculatorsToRun, calculatorRegistry[k])
		}
	} else {
		// En mode simple, on récupère l'algorithme demandé depuis le "Registry".
		calculator, ok := calculatorRegistry[algo]
		if !ok {
			fmt.Fprintf(out, "Erreur : algorithme '%s' inconnu.\n", config.Algo)
			return ExitErrorGeneric
		}
		fmt.Fprintf(out, "Mode : Simple exécution.\nAlgorithme : %s\n", calculator.Name())
		calculatorsToRun = append(calculatorsToRun, calculator)
	}

	fmt.Fprintf(out, "\n--- Exécution ---\n")

	// --- EXÉCUTION ET ANALYSE ---
	// L'orchestration complexe est déléguée à d'autres fonctions.
	results := executeCalculations(ctx, calculatorsToRun, config, out)

	// Si un seul calcul a été effectué, on affiche directement son résultat.
	if len(results) == 1 {
		res := results[0]
		fmt.Fprintln(out, "\n--- Résultat Final ---")
		if res.Err != nil {
			// La gestion des erreurs est centralisée pour déterminer le bon code de sortie.
			return handleCalculationError(res.Err, res.Duration, config.Timeout, out)
		}
		cli.DisplayResult(res.Result, config.N, res.Duration, config.Verbose, out)
		return ExitSuccess
	}

	// Si plusieurs calculs ont été effectués, on procède à une analyse comparative.
	return analyzeComparisonResults(results, config, out)
}

// executeCalculations orchestre l'exécution parallèle des calculs.
// C'est un exemple parfait de "concurrence structurée" en Go.
func executeCalculations(ctx context.Context, calculators []fibonacci.Calculator, config AppConfig, out io.Writer) []CalculationResult {
	// EXPLICATION ACADÉMIQUE : `errgroup.WithContext`
	// Crée un groupe de goroutines et un nouveau contexte dérivé de `ctx`.
	// Ce nouveau contexte sera annulé si :
	//  a) le contexte parent `ctx` est annulé (timeout, Ctrl+C).
	//  b) l'une des goroutines du groupe retourne une erreur non-nulle.
	// C'est le mécanisme qui permet le "fail-fast".
	g, ctx := errgroup.WithContext(ctx)

	results := make([]CalculationResult, len(calculators))
	// Canal pour agréger les mises à jour de progression de toutes les goroutines.
	// Il est "buffered" pour éviter que les goroutines de calcul ne bloquent si le
	// consommateur (l'afficheur de progression) est momentanément occupé.
	progressChan := make(chan fibonacci.ProgressUpdate, len(calculators)*10)

	// --- PATRON FAN-OUT : Lancement des tâches de calcul ---
	// On "distribue" le travail à plusieurs goroutines (les "workers").
	for i, calc := range calculators {
		// EXPLICATION ACADÉMIQUE : Capture des variables de boucle
		// Il est CRUCIAL de créer des copies locales des variables de boucle (`i` et `calc`)
		// avant de les utiliser dans une goroutine.
		// `idx := i` et `calculator := calc`
		// Si on ne le faisait pas, toutes les goroutines partageraient les mêmes variables,
		// et au moment où elles s'exécutent, la boucle serait probablement terminée,
		// et toutes les goroutines utiliseraient la dernière valeur de `i` et `calc`.
		idx := i
		calculator := calc

		// `g.Go()` lance une fonction dans une nouvelle goroutine gérée par le groupe.
		g.Go(func() error {
			startTime := time.Now()

			// L'appel bloquant au calcul. Le contexte `ctx` est passé pour permettre
			// l'annulation à l'intérieur de l'algorithme.
			res, err := calculator.Calculate(ctx, progressChan, idx, config.N, config.Threshold)

			// Le résultat est stocké dans le slice. C'est "thread-safe" car chaque
			// goroutine écrit dans un index unique et prédéterminé (`results[idx]`),
			// évitant ainsi une "race condition".
			results[idx] = CalculationResult{
				Name:     calculator.Name(),
				Result:   res,
				Duration: time.Since(startTime),
				Err:      err,
			}
			// Si `err` est non-nul, `errgroup` se chargera d'annuler le contexte pour
			// les autres goroutines. Si `err` est `nil`, cela signale simplement
			// que cette goroutine a terminé avec succès.
			return err
		})
	}

	// --- GESTION DE L'AFFICHAGE (Consommateur) ---
	// La logique d'affichage de la progression est lancée dans sa propre goroutine.
	// Elle consommera les messages du `progressChan` jusqu'à ce qu'il soit fermé.
	var displayWg sync.WaitGroup
	displayWg.Add(1)
	go cli.DisplayAggregateProgress(&displayWg, progressChan, len(calculators), out)

	// --- SÉQUENCE DE FERMETURE (Coordination) ---

	// 1. Attendre la fin de toutes les tâches de calcul.
	// `g.Wait()` bloque jusqu'à ce que toutes les goroutines lancées avec `g.Go()`
	// aient terminé. Elle retourne la première erreur non-nulle rencontrée.
	// Ici, on ignore l'erreur car elle est déjà gérée par le contexte et
	// stockée dans le slice `results`.
	_ = g.Wait()

	// 2. Fermer le canal central.
	// C'est une étape critique. On ferme `progressChan` pour signaler au consommateur
	// (l'afficheur) qu'il n'y aura plus de messages. C'est sûr de le faire ici
	// car `g.Wait()` garantit que tous les producteurs (les calculateurs) ont terminé.
	close(progressChan)

	// 3. Attendre la fin de l'affichage.
	// On attend que la goroutine d'affichage ait fini de traiter tous les messages
	// restants dans le canal et se termine.
	displayWg.Wait()

	return results
}

// analyzeComparisonResults valide l'intégrité des résultats en mode "all".
func analyzeComparisonResults(results []CalculationResult, config AppConfig, out io.Writer) int {
	fmt.Fprintln(out, "\n--- Résultats de la Comparaison (Benchmark & Validation) ---")

	var firstResult *big.Int
	var firstError error
	successCount := 0

	// Tri des résultats par durée pour un affichage intuitif (du plus rapide au plus lent).
	// C'est une bonne pratique UX pour un outil de benchmark.
	sort.Slice(results, func(i, j int) bool {
		// Gère les erreurs pour qu'elles apparaissent en dernier.
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
		// On utilise la première erreur rencontrée pour le rapport.
		return handleCalculationError(firstError, 0, config.Timeout, out)
	}

	// Validation de la cohérence : c'est une étape de test d'intégration "live".
	// On vérifie que tous les algorithmes qui ont réussi ont produit le même résultat.
	mismatch := false
	for _, res := range results {
		// `big.Int.Cmp` est la méthode correcte pour comparer des `big.Int`.
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
	// Affiche le premier résultat valide (ils sont tous identiques).
	cli.DisplayResult(firstResult, config.N, 0, config.Verbose, out)
	return ExitSuccess
}

// handleCalculationError interprète les erreurs et détermine le code de sortie approprié.
func handleCalculationError(err error, duration time.Duration, timeout time.Duration, out io.Writer) int {
	if err == nil {
		return ExitSuccess
	}

	msg := ""
	if duration > 0 {
		msg = fmt.Sprintf(" après %s", duration)
	}

	// EXPLICATION ACADÉMIQUE : `errors.Is`
	// `errors.Is` est la manière idiomatique et robuste de vérifier les erreurs en Go.
	// Elle permet de vérifier si une erreur dans une chaîne d'erreurs (wrapping)
	// correspond à un type ou une valeur d'erreur spécifique. C'est plus fiable
	// qu'une simple comparaison `err == context.DeadlineExceeded`, car l'erreur
	// pourrait avoir été "enveloppée" (wrapped) par une autre erreur.
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
