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

// DisplayAggregateProgress gère l'affichage dynamique de la barre de progression.
func DisplayAggregateProgress(wg *sync.WaitGroup, progressChan <-chan fibonacci.ProgressUpdate, numCalculators int, out io.Writer) {
	defer wg.Done()
	progresses := make([]float64, numCalculators)

	// Taux de Rafraîchissement Limité (10Hz).
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

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
		// \r (Retour Chariot) pour réécrire sur la même ligne.
		fmt.Fprintf(out, "\r%s : %6.2f%% [%-30s]", label, avgProgress*100, progressBar(avgProgress, 30))
	}

	// Boucle d'événements principale (Pattern Select).
	for {
		select {
		case update, ok := <-progressChan:
			// Si `ok` est faux, le canal est fermé (signal de terminaison).
			if !ok {
				// Affichage final à 100%.
				for i := range progresses {
					progresses[i] = 1.0
				}
				printBar()
				fmt.Fprintln(out)
				return
			}
			if update.CalculatorIndex < len(progresses) {
				progresses[update.CalculatorIndex] = update.Value
			}

		case <-ticker.C:
			printBar()
		}
	}
}

func progressBar(progress float64, length int) string {
	if progress > 1.0 {
		progress = 1.0
	} else if progress < 0.0 {
		progress = 0.0
	}
	count := int(progress * float64(length))
	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		if i < count {
			builder.WriteRune('■')
		} else {
			builder.WriteRune(' ')
		}
	}
	return builder.String()
}

// DisplayResult formate et affiche le résultat final F(n).
func DisplayResult(result *big.Int, n uint64, duration time.Duration, verbose bool, out io.Writer) {
	fmt.Fprintln(out, "\n--- Données du Résultat ---")
	if duration > 0 {
		fmt.Fprintf(out, "Durée d'exécution : %s\n", duration)
	}

	bitLen := result.BitLen()
	fmt.Fprintf(out, "Taille Binaire : %d bits.\n", bitLen)

	// Conversion en base 10 (coûteux mais nécessaire pour l'affichage).
	resultStr := result.String()
	numDigits := len(resultStr)
	fmt.Fprintf(out, "Nombre de Chiffres Décimaux : %d\n", numDigits)

	const truncationLimit = 80
	const displayEdges = 20

	if verbose {
		fmt.Fprintf(out, "\nF(%d) = %s\n", n, resultStr)
	} else if numDigits > truncationLimit {
		fmt.Fprintf(out, "F(%d) (Tronqué) = %s...%s\n", n, resultStr[:displayEdges], resultStr[numDigits-displayEdges:])
	} else {
		fmt.Fprintf(out, "F(%d) = %s\n", n, resultStr)
	}
}
