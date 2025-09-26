// EXPLICATION ACADÉMIQUE :
// Le package `cli` (Command-Line Interface) implémente la couche de présentation.
// Il est responsable de l'interaction avec l'utilisateur dans le terminal (affichage de la
// progression, formatage des résultats). Le séparer de la logique métier (`fibonacci`)
// respecte le principe de "Séparation des préoccupations", augmentant la modularité et la testabilité.
package cli

import (
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"example.com/fibcalc/internal/fibonacci"
)

// Constantes pour la configuration de l'interface utilisateur.
const (
	// Configuration de l'affichage de la progression
	ProgressRefreshRate = 100 * time.Millisecond // Taux de rafraîchissement (10Hz). Fluide sans surcharger le CPU.
	ProgressBarWidth    = 40                     // Largeur visuelle de la barre de progression.

	// Configuration de la troncature des résultats
	TruncationLimit = 100 // Au-delà de cette limite, le résultat est tronqué si le mode verbose n'est pas activé.
	DisplayEdges    = 25  // Nombre de chiffres à afficher au début et à la fin lors de la troncature.
)

// [REFACTORING MAJEUR] : Encapsulation de l'état de la progression.

// ProgressState gère l'état agrégé et l'affichage de la progression.
type ProgressState struct {
	progresses     []float64
	numCalculators int
	out            io.Writer
}

// NewProgressState initialise l'état.
func NewProgressState(numCalculators int, out io.Writer) *ProgressState {
	return &ProgressState{
		progresses:     make([]float64, numCalculators),
		numCalculators: numCalculators,
		out:            out,
	}
}

// Update met à jour la progression d'un calculateur spécifique de manière sécurisée.
func (ps *ProgressState) Update(index int, value float64) {
	// Vérification des bornes pour éviter un panic si l'index est invalide.
	if index >= 0 && index < len(ps.progresses) {
		ps.progresses[index] = value
	}
}

// CalculateAverage calcule la progression moyenne actuelle.
func (ps *ProgressState) CalculateAverage() float64 {
	var totalProgress float64
	for _, p := range ps.progresses {
		totalProgress += p
	}
	// Protection robuste contre la division par zéro.
	if ps.numCalculators == 0 {
		return 0.0
	}
	return totalProgress / float64(ps.numCalculators)
}

// PrintBar dessine la barre de progression actuelle.
func (ps *ProgressState) PrintBar(final bool) {
	avgProgress := ps.CalculateAverage()
	label := "Progression"
	if ps.numCalculators > 1 {
		label = "Progression Moyenne"
	}

	bar := progressBar(avgProgress, ProgressBarWidth)

	// EXPLICATION ACADÉMIQUE : Manipulation du Curseur et Séquences ANSI
	// `\r` (Retour Chariot) ramène le curseur au début de la ligne.
	// [BONIFICATION] `\033[K` (Erase in Line) est une séquence d'échappement ANSI qui efface la ligne
	// depuis le curseur jusqu'à la fin. C'est plus robuste que `\r` seul, car cela
	// garantit qu'aucun "résidu" de la ligne précédente ne reste si la nouvelle ligne est plus courte.
	fmt.Fprintf(ps.out, "\r\033[K%s : %6.2f%% [%s]", label, avgProgress*100, bar)

	if final {
		fmt.Fprintln(ps.out) // Passe à la ligne suivante pour préserver la barre finale.
	}
}

// DisplayAggregateProgress gère l'affichage dynamique et agrégé.
// Elle est conçue pour être lancée dans sa propre goroutine (le "Consommateur").
func DisplayAggregateProgress(wg *sync.WaitGroup, progressChan <-chan fibonacci.ProgressUpdate, numCalculators int, out io.Writer) {
	// Garantit que le WaitGroup est décrémenté à la fin.
	defer wg.Done()

	// [BONIFICATION] Gestion du cas limite et Robustesse Concurrente.
	if numCalculators <= 0 {
		// CRUCIAL : Il faut consommer (drainer) le canal jusqu'à sa fermeture pour ne pas
		// bloquer l'appelant (l'orchestrateur) qui pourrait attendre avant de fermer le canal.
		for range progressChan {
			// Ne rien faire, juste consommer.
		}
		return
	}

	state := NewProgressState(numCalculators, out)

	// EXPLICATION ACADÉMIQUE : Limitation de Taux (Rate Limiting)
	// Un `time.Ticker` est utilisé pour limiter le rafraîchissement de l'UI, évitant
	// de surcharger le terminal (scintillement/"flickering").
	ticker := time.NewTicker(ProgressRefreshRate)
	defer ticker.Stop() // Libère les ressources du ticker.

	// EXPLICATION ACADÉMIQUE : Boucle d'Événements avec `select`
	// Le cœur de la concurrence réactive en Go. Permet d'attendre et de réagir
	// simultanément à plusieurs événements (réception d'une mise à jour OU déclenchement du ticker).
	for {
		select {
		// Cas 1: Une nouvelle mise à jour est reçue ou le canal est fermé.
		case update, ok := <-progressChan:
			// Détection de la fermeture du canal (`ok == false`).
			if !ok {
				// Signal de terminaison : les producteurs ont fini.
				// [REFACTORING] On affiche l'état final réel (qui peut être < 100% si annulé).
				state.PrintBar(true)
				return
			}
			// Mise à jour de l'état interne. L'accès à l'état est sûr car le `select` sérialise les opérations.
			state.Update(update.CalculatorIndex, update.Value)

		// Cas 2: Le ticker a "sonné".
		case <-ticker.C:
			// Rafraîchissement de l'affichage avec les dernières valeurs reçues.
			state.PrintBar(false)
		}
	}
}

// progressBar génère une représentation textuelle d'une barre de progression.
func progressBar(progress float64, length int) string {
	// "Clamping" de la valeur dans l'intervalle [0.0, 1.0].
	if progress > 1.0 {
		progress = 1.0
	} else if progress < 0.0 {
		progress = 0.0
	}

	// [BONIFICATION] Utilisation de runes Unicode pour un rendu visuel moderne.
	const (
		filledChar = '█' // U+2588 (Full Block)
		emptyChar  = '░' // U+2591 (Light Shade) - plus visible qu'un espace.
	)

	count := int(progress * float64(length))

	// EXPLICATION ACADÉMIQUE : Efficacité avec `strings.Builder`
	// `strings.Builder` minimise les allocations mémoire par rapport à la concaténation (`+`).
	var builder strings.Builder

	// [OPTIMISATION] Pré-allocation de la mémoire avec `builder.Grow()`.
	// Puisque les runes Unicode peuvent prendre plusieurs octets en UTF-8 (ici 3 octets pour █ et ░),
	// on alloue `length * 3` pour éviter toute réallocation dynamique pendant la boucle.
	builder.Grow(length * 3)

	for i := 0; i < length; i++ {
		if i < count {
			builder.WriteRune(filledChar)
		} else {
			builder.WriteRune(emptyChar)
		}
	}
	return builder.String()
}

// DisplayResult formate et affiche le résultat final F(n) de manière lisible.
func DisplayResult(result *big.Int, n uint64, duration time.Duration, verbose bool, out io.Writer) {
	fmt.Fprintln(out, "\n--- Données du Résultat ---")

	// Affichage de la durée si pertinente (non nulle).
	if duration > 0 {
		fmt.Fprintf(out, "Durée d'exécution     : %s\n", duration)
	}

	// `BitLen()` donne la taille binaire de manière efficace.
	bitLen := result.BitLen()
	// [BONIFICATION] Amélioration de la lisibilité des métadonnées.
	fmt.Fprintf(out, "Taille Binaire        : %s bits.\n", formatNumberString(fmt.Sprintf("%d", bitLen)))

	// EXPLICATION ACADÉMIQUE : Coût des conversions Base 2 -> Base 10
	// La conversion d'un `big.Int` en chaîne décimale (`result.String()`) est coûteuse
	// (complexité quasi-linéaire). Il faut l'appeler une seule fois.
	resultStr := result.String()
	numDigits := len(resultStr)
	fmt.Fprintf(out, "Chiffres Décimaux     : %s\n", formatNumberString(fmt.Sprintf("%d", numDigits)))

	// [BONIFICATION] Notation Scientifique
	// Donne une idée immédiate et précise de l'ordre de grandeur pour les grands nombres.
	if numDigits > 6 {
		// On utilise `big.Float` pour le calcul en virgule flottante de haute précision.
		f := new(big.Float).SetInt(result)
		// Le format '%e' fournit la notation scientifique standard (e.g., 1.234567e+08).
		fmt.Fprintf(out, "Notation Scientifique : %e\n", f)
	}

	// Gestion de l'affichage du résultat complet ou tronqué (UX).
	fmt.Fprintln(out, "\n--- Valeur Calculée ---")
	if verbose {
		// Mode verbeux : Affichage complet avec séparateurs de milliers.
		fmt.Fprintf(out, "F(%d) =\n%s\n", n, formatNumberString(resultStr))
	} else if numDigits > TruncationLimit {
		// Troncature pour éviter d'inonder le terminal.
		fmt.Fprintf(out, "F(%d) (Tronqué) = %s...%s\n", n, resultStr[:DisplayEdges], resultStr[numDigits-DisplayEdges:])
		fmt.Fprintln(out, "(Utilisez le flag -v ou --verbose pour afficher le résultat complet)")
	} else {
		// Résultat court : Affichage complet avec séparateurs.
		fmt.Fprintf(out, "F(%d) = %s\n", n, formatNumberString(resultStr))
	}
}

// [BONIFICATION] formatNumberString ajoute des séparateurs de milliers (virgules)
// à une chaîne représentant un nombre décimal. Implémentation efficace.
func formatNumberString(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// Calcul de la capacité nécessaire : n chiffres + (n-1)/3 séparateurs.
	var builder strings.Builder
	builder.Grow(n + (n-1)/3)

	// Gérer le premier groupe (qui peut être < 3 chiffres).
	// Ex: Pour "12345", n=5. firstGroupLen = 5 % 3 = 2 ("12").
	firstGroupLen := n % 3
	if firstGroupLen == 0 {
		firstGroupLen = 3 // Si divisible par 3 (e.g., "123456"), le premier groupe est 3.
	}

	// Écriture du premier groupe.
	builder.WriteString(s[:firstGroupLen])

	// Écriture des groupes restants, précédés d'une virgule.
	for i := firstGroupLen; i < n; i += 3 {
		builder.WriteByte(',')
		builder.WriteString(s[i : i+3])
	}

	return builder.String()
}
