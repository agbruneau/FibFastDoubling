// EXPLICATION ACADÉMIQUE :
// Le package `cli` (Command-Line Interface) est responsable de toute l'interaction
// avec l'utilisateur dans le terminal. Le séparer de la logique de calcul (`fibonacci`)
// et de l'orchestration (`main`) est un principe clé de la conception logicielle :
// la "Séparation des préoccupations" (Separation of Concerns).
// Cela rend le code plus modulaire, plus facile à tester et à maintenir. Par exemple,
// on pourrait remplacer cette CLI par une interface web sans toucher au paquet `fibonacci`.
package cli

import (
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"example.com/fibcalc/internal/fibonacci" // Adapter au nom du module
)

// DisplayAggregateProgress gère l'affichage dynamique et agrégé de la progression
// de plusieurs calculs concurrents.
// Il est conçu pour être lancé dans sa propre goroutine.
//
// EXPLICATION ACADÉMIQUE : Paramètres de la fonction
//   - `wg *sync.WaitGroup`: Permet à l'appelant d'attendre que cette goroutine ait
//     fini son travail (par exemple, après la fermeture du canal).
//   - `progressChan <-chan fibonacci.ProgressUpdate`: Un canal en lecture seule (`<-chan`)
//     qui sert de source de données pour les mises à jour de progression.
//   - `numCalculators int`: Le nombre de producteurs qui envoient des données sur le canal.
//   - `out io.Writer`: Une interface pour la sortie, ce qui permet de tester facilement
//     la fonction en lui passant un buffer en mémoire (`bytes.Buffer`) au lieu de
//     la sortie standard (`os.Stdout`).
func DisplayAggregateProgress(wg *sync.WaitGroup, progressChan <-chan fibonacci.ProgressUpdate, numCalculators int, out io.Writer) {
	// `defer wg.Done()` garantit que le WaitGroup est décrémenté à la fin de la fonction,
	// signalant à l'appelant que cette goroutine a terminé son exécution.
	defer wg.Done()
	progresses := make([]float64, numCalculators)

	// EXPLICATION ACADÉMIQUE : Limitation de Taux (Rate Limiting)
	// Afficher une mise à jour à chaque fois qu'on reçoit une donnée peut surcharger
	// le terminal et provoquer un scintillement désagréable ("flickering").
	// Un `time.Ticker` est l'outil idiomatique en Go pour exécuter une action à
	// intervalle régulier. Ici, on limite le rafraîchissement de la barre de
	// progression à 10 fois par seconde (toutes les 100ms), ce qui est fluide
	// pour l'œil humain sans consommer trop de CPU.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop() // Libère les ressources associées au ticker.

	// `printBar` est une closure qui capture les variables `progresses`, `numCalculators` et `out`.
	printBar := func() {
		var totalProgress float64
		for _, p := range progresses {
			totalProgress += p
		}
		avgProgress := totalProgress / float64(numCalculators)

		label := "Progression"
		if numCalculators > 1 {
			label = "Progression Moyenne"
		}
		// EXPLICATION ACADÉMIQUE : Manipulation du Curseur du Terminal
		// Le caractère spécial `\r` (Retour Chariot / Carriage Return) déplace le
		// curseur au début de la ligne actuelle sans passer à la ligne suivante.
		// La prochaine écriture (`fmt.Fprintf`) écrasera donc la ligne précédente,
		// créant l'illusion d'une mise à jour sur place. C'est une technique
		// simple et multi-plateforme pour les animations en console.
		fmt.Fprintf(out, "\r%s : %6.2f%% [%-30s]", label, avgProgress*100, progressBar(avgProgress, 30))
	}

	// EXPLICATION ACADÉMIQUE : Boucle d'Événements avec `select`
	// C'est un des patrons de conception les plus puissants et courants en Go.
	// La boucle `for { select { ... } }` permet à une goroutine d'attendre et de
	// réagir à plusieurs sources d'événements (ici, des canaux) simultanément.
	for {
		select {
		// Cas 1: Une nouvelle mise à jour de progression est reçue.
		case update, ok := <-progressChan:
			// La forme `val, ok := <-ch` permet de détecter si un canal a été fermé.
			// Si `ok` est `false`, cela signifie que les producteurs ont terminé et
			// que le canal a été fermé par l'appelant. C'est le signal de terminaison.
			if !ok {
				// Affichage final à 100% pour une finition propre.
				for i := range progresses {
					progresses[i] = 1.0
				}
				printBar()
				fmt.Fprintln(out) // Passe à la ligne suivante pour ne pas écraser la barre finale.
				return            // Termine la goroutine.
			}
			if update.CalculatorIndex < len(progresses) {
				progresses[update.CalculatorIndex] = update.Value
			}

		// Cas 2: Le ticker a "sonné" (100ms se sont écoulées).
		case <-ticker.C:
			// On redessine la barre de progression avec les dernières valeurs reçues.
			printBar()
		}
	}
}

// progressBar génère une représentation textuelle simple d'une barre de progression.
func progressBar(progress float64, length int) string {
	// Clamp progress to the [0.0, 1.0] range.
	if progress > 1.0 {
		progress = 1.0
	} else if progress < 0.0 {
		progress = 0.0
	}
	count := int(progress * float64(length))

	// EXPLICATION ACADÉMIQUE : `strings.Builder`
	// Pour construire des chaînes de caractères par concaténation en boucle, il est
	// beaucoup plus performant d'utiliser `strings.Builder` que l'opérateur `+`.
	// `strings.Builder` pré-alloue un buffer et minimise les allocations de mémoire
	// répétées, ce qui est plus efficace. `builder.Grow(length)` est une
	// optimisation supplémentaire qui alloue la capacité nécessaire en une seule fois.
	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		if i < count {
			builder.WriteRune('■') // Caractère plein
		} else {
			builder.WriteRune(' ') // Caractère vide
		}
	}
	return builder.String()
}

// DisplayResult formate et affiche le résultat final F(n) de manière lisible.
func DisplayResult(result *big.Int, n uint64, duration time.Duration, verbose bool, out io.Writer) {
	fmt.Fprintln(out, "\n--- Données du Résultat ---")
	if duration > 0 {
		fmt.Fprintf(out, "Durée d'exécution : %s\n", duration)
	}

	// `BitLen()` est une méthode efficace pour connaître l'ordre de grandeur d'un `big.Int`.
	bitLen := result.BitLen()
	fmt.Fprintf(out, "Taille Binaire : %d bits.\n", bitLen)

	// EXPLICATION ACADÉMIQUE : Coût des conversions
	// La conversion d'un `big.Int` en chaîne de caractères décimale (`result.String()`)
	// est une opération coûteuse, car elle implique des divisions répétées.
	// Il est bon d'en être conscient et de ne l'appeler qu'une seule fois si possible.
	resultStr := result.String()
	numDigits := len(resultStr)
	fmt.Fprintf(out, "Nombre de Chiffres Décimaux : %d\n", numDigits)

	const truncationLimit = 80
	const displayEdges = 20

	// Pour des raisons d'ergonomie (UX), on évite d'inonder le terminal avec des milliers
	// de chiffres. Si le nombre est trop grand, on le tronque en affichant uniquement
	// le début et la fin, sauf si l'utilisateur a explicitement demandé le résultat
	// complet avec le flag `-v` (verbose).
	if verbose {
		fmt.Fprintf(out, "\nF(%d) = %s\n", n, resultStr)
	} else if numDigits > truncationLimit {
		fmt.Fprintf(out, "F(%d) (Tronqué) = %s...%s\n", n, resultStr[:displayEdges], resultStr[numDigits-displayEdges:])
	} else {
		fmt.Fprintf(out, "F(%d) = %s\n", n, resultStr)
	}
}
