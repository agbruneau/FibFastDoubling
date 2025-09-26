//
// MODULE ACADÉMIQUE : COUCHE DE PRÉSENTATION (UI) EN GO
//
// OBJECTIF PÉDAGOGIQUE :
// Ce module, `cli`, illustre la conception d'une interface utilisateur en ligne de commande (CLI)
// robuste et performante en Go. Il met en lumière des techniques essentielles pour gérer
// l'affichage concurrentiel, l'interaction non bloquante, et le formatage de données complexes.
//
// CONCEPTS CLÉS DÉMONTRÉS :
//  1. SÉPARATION DES PRÉOCCUPATIONS : L'UI (`cli`) est totalement découplée de la logique
//     métier (`fibonacci`). Elle communique uniquement via des structures de données
//     bien définies (`fibonacci.ProgressUpdate`) et des canaux, un principe fondamental
//     pour la modularité et la testabilité.
//  2. MODÈLE PRODUCTEUR/CONSOMMATEUR : L'orchestrateur (`main.go`) et les calculateurs
//     sont les "Producteurs" de messages de progression. La fonction `DisplayAggregateProgress`
//     est le "Consommateur", s'exécutant dans sa propre goroutine pour traiter ces messages
//     de manière asynchrone.
//  3. CONCURRENCE SÉRIALISÉE : L'accès à l'état partagé (`ProgressState`) est rendu "thread-safe"
//     sans utiliser de mutex explicite. La goroutine unique du consommateur garantit que
//     toutes les mises à jour de l'état sont traitées séquentiellement.
//  4. GESTION DE L'AFFICHAGE (RATE LIMITING) : L'utilisation d'un `time.Ticker` pour limiter
//     la fréquence de rafraîchissement du terminal est une pratique cruciale pour éviter
//     le scintillement ("flickering") et réduire la charge CPU inutile.
//  5. MANIPULATION DU TERMINAL : L'usage de séquences d'échappement ANSI (`\r`, `\033[K`)
//     démontre comment créer des interfaces dynamiques et professionnelles dans un terminal.
//
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

// Constantes définissant le comportement et l'apparence de l'interface utilisateur.
const (
	// ProgressRefreshRate définit la fréquence de rafraîchissement de la barre de progression.
	// Une valeur de 100ms (10Hz) offre un bon compromis entre une animation fluide
	// et une consommation CPU minimale. Un taux trop élevé pourrait surcharger le terminal.
	ProgressRefreshRate = 100 * time.Millisecond

	// ProgressBarWidth détermine la largeur en caractères de la barre de progression.
	ProgressBarWidth = 40

	// TruncationLimit est le nombre de chiffres au-delà duquel un résultat est tronqué
	// par défaut pour ne pas submerger l'affichage du terminal.
	TruncationLimit = 100

	// DisplayEdges spécifie combien de chiffres afficher au début et à la fin d'un nombre tronqué.
	DisplayEdges = 25
)

// ProgressState encapsule l'état complet de la progression de tous les calculs en cours.
// Cette structure agit comme un "modèle" dans une architecture Modèle-Vue-Contrôleur (MVC) simplifiée,
// où elle détient les données à afficher.
type ProgressState struct {
	// Le slice `progresses` stocke la dernière valeur de progression (0.0 à 1.0) pour chaque calculateur.
	// L'index du slice correspond au `CalculatorIndex` reçu dans les messages.
	progresses     []float64
	numCalculators int
	out            io.Writer // La destination de sortie (e.g., os.Stdout), injectée pour la testabilité.
}

// NewProgressState est une factory qui initialise un nouvel état de progression.
func NewProgressState(numCalculators int, out io.Writer) *ProgressState {
	return &ProgressState{
		progresses:     make([]float64, numCalculators),
		numCalculators: numCalculators,
		out:            out,
	}
}

// Update met à jour la progression pour un calculateur spécifique.
// Cette méthode est appelée par la goroutine du consommateur, garantissant un accès séquentiel
// et évitant ainsi les "race conditions" sans nécessiter de verrou (mutex).
func (ps *ProgressState) Update(index int, value float64) {
	// La validation des bornes est une mesure de robustesse essentielle pour prévenir
	// un "panic: index out of range" si un index invalide était reçu.
	if index >= 0 && index < len(ps.progresses) {
		ps.progresses[index] = value
	}
}

// CalculateAverage calcule la progression moyenne de tous les calculateurs.
// Utile pour le mode "benchmark" où plusieurs algorithmes tournent en parallèle.
func (ps *ProgressState) CalculateAverage() float64 {
	var totalProgress float64
	for _, p := range ps.progresses {
		totalProgress += p
	}
	// La protection contre la division par zéro est cruciale.
	if ps.numCalculators == 0 {
		return 0.0
	}
	return totalProgress / float64(ps.numCalculators)
}

// PrintBar génère et affiche la représentation textuelle de la barre de progression.
func (ps *ProgressState) PrintBar(final bool) {
	avgProgress := ps.CalculateAverage()
	label := "Progression"
	if ps.numCalculators > 1 {
		label = "Progression Moyenne"
	}

	bar := progressBar(avgProgress, ProgressBarWidth)

	// EXPLICATION ACADÉMIQUE : Manipulation du Curseur du Terminal
	// Les séquences d'échappement ANSI sont des commandes spéciales pour contrôler le terminal.
	// - `\r` (Retour Chariot / Carriage Return) : Déplace le curseur au début de la ligne actuelle
	//   SANS effacer son contenu.
	// - `\033[K` (Erase in Line) : Efface la ligne depuis la position actuelle du curseur
	//   jusqu'à la fin.
	// La combinaison `\r\033[K` est une technique robuste pour s'assurer que la nouvelle ligne
	// remplace complètement l'ancienne, même si la nouvelle est plus courte.
	fmt.Fprintf(ps.out, "\r\033[K%s : %6.2f%% [%s]", label, avgProgress*100, bar)

	// Si c'est l'affichage final, on ajoute un saut de ligne (`\n`) pour que la barre
	// reste visible et que les affichages suivants apparaissent sur une nouvelle ligne.
	if final {
		fmt.Fprintln(ps.out)
	}
}

// DisplayAggregateProgress est le cœur du consommateur de l'UI.
// Elle s'exécute dans une goroutine dédiée et gère le cycle de vie de l'affichage de la progression.
func DisplayAggregateProgress(wg *sync.WaitGroup, progressChan <-chan fibonacci.ProgressUpdate, numCalculators int, out io.Writer) {
	// `defer wg.Done()` est une instruction cruciale qui garantit que le WaitGroup
	// est notifié de la fin de cette goroutine, même si elle se termine par une erreur
	// ou un `return` prématuré. C'est le mécanisme de synchronisation avec l'appelant.
	defer wg.Done()

	// Cas limite : si aucun calculateur n'est lancé, on ne fait rien.
	// Cependant, il est VITAL de vider le canal (`drain the channel`). Si l'appelant
	// envoie des messages avant de fermer le canal, il pourrait se bloquer indéfiniment
	// si personne ne lit ces messages. Cette boucle `for range` lit et ignore tous les
	// messages jusqu'à la fermeture du canal.
	if numCalculators <= 0 {
		for range progressChan {
		}
		return
	}

	state := NewProgressState(numCalculators, out)

	// EXPLICATION ACADÉMIQUE : Limitation de Taux (Rate Limiting) avec time.Ticker
	// Un Ticker envoie un événement sur son canal (`ticker.C`) à intervalles réguliers.
	// C'est la méthode idiomatique en Go pour exécuter une action périodique.
	// Cela évite de redessiner l'UI à chaque micro-mise à jour reçue, ce qui causerait
	// un scintillement intense ("flickering") et une utilisation CPU élevée.
	ticker := time.NewTicker(ProgressRefreshRate)
	// `defer ticker.Stop()` est essentiel pour libérer les ressources internes (timers, goroutines)
	// associées au ticker lorsque la fonction se termine.
	defer ticker.Stop()

	// EXPLICATION ACADÉMIQUE : Boucle d'Événements avec `select`
	// C'est un des patrons de concurrence les plus puissants en Go. Le `select` bloque
	// jusqu'à ce qu'UN de ses `case` soit prêt (non-bloquant).
	// Cela permet de gérer plusieurs sources d'événements asynchrones de manière élégante.
	for {
		select {
		// CAS 1 : Un message a été reçu sur le canal `progressChan`.
		// La syntaxe `update, ok := <-progressChan` est idiomatique pour lire d'un canal.
		// `ok` est `false` si le canal a été fermé et est vide.
		case update, ok := <-progressChan:
			if !ok {
				// Le canal est fermé : c'est le signal de fin envoyé par le producteur.
				// On effectue un dernier affichage pour montrer l'état final (qui peut
				// être < 100% en cas d'annulation) avant de terminer la goroutine.
				state.PrintBar(true)
				return // Termine la fonction et la goroutine.
			}
			// Un nouveau message de progression est arrivé. On met à jour notre modèle de données.
			state.Update(update.CalculatorIndex, update.Value)

		// CAS 2 : Le ticker a envoyé un événement.
		case <-ticker.C:
			// Il est temps de rafraîchir l'affichage. On redessine la barre de progression
			// avec les dernières données de progression accumulées.
			state.PrintBar(false)
		}
	}
}

// progressBar génère une chaîne de caractères représentant une barre de progression.
func progressBar(progress float64, length int) string {
	// Le "clamping" (bornage) de la valeur entre 0.0 et 1.0 est une mesure de robustesse
	// pour éviter des calculs invalides si une valeur aberrante est fournie.
	if progress > 1.0 {
		progress = 1.0
	} else if progress < 0.0 {
		progress = 0.0
	}

	// L'utilisation de runes Unicode (█ et ░) offre un rendu visuel plus agréable
	// et moderne que les caractères ASCII standards (comme # et -).
	const (
		filledChar = '█' // U+2588 : Bloc plein
		emptyChar  = '░' // U+2591 : Ombre légère
	)

	count := int(progress * float64(length))

	// EXPLICATION ACADÉMIQUE : Optimisation de la Construction de Chaînes
	// En Go, la concaténation de chaînes avec l'opérateur `+` dans une boucle est inefficace
	// car elle crée une nouvelle chaîne (et donc une nouvelle allocation mémoire) à chaque itération.
	// `strings.Builder` est conçu pour résoudre ce problème. Il utilise un buffer interne
	// (un slice de bytes) qui est agrandi au besoin, minimisant ainsi les allocations.
	var builder strings.Builder

	// OPTIMISATION AVANCÉE : `builder.Grow()`
	// On peut encore améliorer les performances en pré-allouant la taille finale du buffer.
	// Les runes utilisées ici peuvent occuper jusqu'à 3 octets en encodage UTF-8.
	// En allouant `length * 3` octets, on s'assure que le buffer n'aura probablement
	// jamais besoin d'être réalloué dynamiquement pendant la construction de la barre.
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

// DisplayResult formate et affiche le résultat final F(n) et ses métadonnées.
func DisplayResult(result *big.Int, n uint64, duration time.Duration, verbose bool, out io.Writer) {
	fmt.Fprintln(out, "\n--- Données du Résultat ---")

	if duration > 0 {
		fmt.Fprintf(out, "Durée d'exécution     : %s\n", duration)
	}

	// `BitLen()` est une méthode très efficace pour obtenir le nombre de bits nécessaires
	// pour représenter un nombre, ce qui est une mesure de sa "taille" en mémoire.
	bitLen := result.BitLen()
	fmt.Fprintf(out, "Taille Binaire        : %s bits.\n", formatNumberString(fmt.Sprintf("%d", bitLen)))

	// EXPLICATION ACADÉMIQUE : Coût des Conversions de Base
	// La conversion d'un `big.Int` (stocké en base 2^64 ou 2^32) en une chaîne de
	// caractères en base 10 (`result.String()`) est une opération coûteuse, avec une
	// complexité d'environ O(N*log(N)) où N est le nombre de bits.
	// Il est donc crucial de n'effectuer cette conversion qu'UNE SEULE FOIS et de
	// stocker le résultat dans une variable pour le réutiliser.
	resultStr := result.String()
	numDigits := len(resultStr)
	fmt.Fprintf(out, "Chiffres Décimaux     : %s\n", formatNumberString(fmt.Sprintf("%d", numDigits)))

	// La notation scientifique donne un ordre de grandeur immédiat pour les très grands nombres.
	// On utilise `big.Float` pour effectuer la conversion avec une haute précision.
	if numDigits > 6 {
		f := new(big.Float).SetInt(result)
		// Le format '%e' est le standard pour la notation scientifique (ex: 1.234567e+08).
		fmt.Fprintf(out, "Notation Scientifique : %e\n", f)
	}

	fmt.Fprintln(out, "\n--- Valeur Calculée ---")
	// La gestion de l'affichage (complet vs. tronqué) est une question d'expérience
	// utilisateur (UX) pour éviter d'inonder le terminal avec des millions de chiffres.
	if verbose {
		fmt.Fprintf(out, "F(%d) =\n%s\n", n, formatNumberString(resultStr))
	} else if numDigits > TruncationLimit {
		fmt.Fprintf(out, "F(%d) (Tronqué) = %s...%s\n", n, resultStr[:DisplayEdges], resultStr[numDigits-DisplayEdges:])
		fmt.Fprintln(out, "(Utilisez le flag -v ou --verbose pour afficher le résultat complet)")
	} else {
		fmt.Fprintf(out, "F(%d) = %s\n", n, formatNumberString(resultStr))
	}
}

// formatNumberString ajoute des séparateurs de milliers à une chaîne numérique pour une meilleure lisibilité.
// L'implémentation est optimisée pour minimiser les allocations mémoire.
func formatNumberString(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// On utilise ici aussi `strings.Builder` pour une construction efficace.
	var builder strings.Builder
	// Calcul de la capacité exacte requise pour éviter les réallocations :
	// `n` pour les chiffres, et `(n-1)/3` pour le nombre de séparateurs.
	builder.Grow(n + (n - 1)/3)

	// Logique de calcul pour le premier groupe de chiffres (qui peut être de 1, 2 ou 3 chiffres).
	// Exemple : pour s = "12345" (n=5), n % 3 = 2. Le premier groupe est "12".
	// Exemple : pour s = "123456" (n=6), n % 3 = 0. On le traite comme un groupe de 3, "123".
	firstGroupLen := n % 3
	if firstGroupLen == 0 {
		firstGroupLen = 3
	}

	// Écriture du premier groupe.
	builder.WriteString(s[:firstGroupLen])

	// Itération sur le reste de la chaîne, par blocs de 3 chiffres.
	for i := firstGroupLen; i < n; i += 3 {
		builder.WriteByte(',')         // Ajout du séparateur.
		builder.WriteString(s[i : i+3]) // Ajout du groupe de 3 chiffres.
	}

	return builder.String()
}