// EXPLICATION ACADÉMIQUE :
// Ce fichier est central dans l'architecture du projet. Il définit les contrats (interfaces)
// et met en place des optimisations fondamentales qui sont partagées par tous les
// algorithmes de calcul de Fibonacci. Il illustre plusieurs concepts avancés de Go.
package fibonacci

import (
	"context"
	"math/big"
	"sync"
)

const (
	// MaxFibUint64 est l'indice maximum de la suite de Fibonacci dont le résultat
	// peut être contenu dans un entier non signé de 64 bits (`uint64`).
	// F(93) est le dernier, F(94) dépasse la capacité. C'est une limite naturelle
	// pour notre première couche d'optimisation (la Lookup Table).
	MaxFibUint64 = 93

	// DefaultParallelThreshold est le seuil (en nombre de bits) à partir duquel
	// les multiplications de `big.Int` seront parallélisées. En dessous de ce seuil,
	// le coût de la création de goroutines et de la synchronisation est supérieur
	// au gain du parallélisme. C'est un paramètre de "tuning" de performance.
	DefaultParallelThreshold = 2048
)

// ProgressUpdate est la structure de données utilisée pour communiquer l'état
// d'avancement d'un calcul à travers un canal.
type ProgressUpdate struct {
	CalculatorIndex int     // Identifie le calculateur (utile en mode "all")
	Value           float64 // Progression de 0.0 à 1.0
}

// === PATRON DE CONCEPTION : INTERFACE & DÉCORATEUR ===

// Calculator définit l'interface publique pour tous les calculateurs.
// Le fait de "programmer pour une interface" permet au code de `main` de manipuler
// n'importe quel type de calculateur de manière agnostique, sans connaître son
// implémentation concrète.
type Calculator interface {
	// Calculate est la méthode principale. Elle prend un contexte pour l'annulation,
	// un canal pour rapporter la progression, l'index du calculateur, l'entier `n`,
	// et le seuil de parallélisme.
	Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// coreCalculator est une interface interne. Elle représente un algorithme de calcul "pur",
// sans les optimisations communes comme la Lookup Table.
type coreCalculator interface {
	CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// FibCalculator est une implémentation de l'interface `Calculator`.
// EXPLICATION ACADÉMIQUE : Patron de conception "Décorateur" (Decorator Pattern)
// `FibCalculator` agit comme un "décorateur". Il "enveloppe" un `coreCalculator`
// et lui ajoute des fonctionnalités supplémentaires (ici, l'optimisation "fast path"
// avec la Lookup Table) de manière transparente.
//
// Avantages :
//   - On évite de dupliquer la logique de la Lookup Table dans chaque algorithme (FastDoubling, Matrix).
//   - On peut "composer" des fonctionnalités. On pourrait avoir plusieurs couches de décorateurs.
//   - Respecte le principe Ouvert/Fermé (Open/Closed Principle) : on peut ajouter de
//     nouvelles fonctionnalités (décorateurs) sans modifier le code existant (les `coreCalculator`).
type FibCalculator struct {
	core coreCalculator
}

// NewCalculator est une "factory function" qui crée et retourne un décorateur
// `FibCalculator` configuré avec un calculateur de cœur.
func NewCalculator(core coreCalculator) Calculator {
	return &FibCalculator{core: core}
}

// Name délègue simplement l'appel à l'objet enveloppé.
func (c *FibCalculator) Name() string {
	return c.core.Name()
}

// Calculate est le cœur du décorateur.
func (c *FibCalculator) Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	// OPTIMISATION "FAST PATH" :
	// C'est une technique d'optimisation très courante. On traite d'abord le cas
	// le plus simple et le plus rapide. Si `n` est suffisamment petit, on prend
	// un "raccourci" (path) et on retourne immédiatement le résultat depuis la
	// Lookup Table, ce qui est une opération en O(1).
	if n <= MaxFibUint64 {
		reportProgress(progressChan, calcIndex, 1.0) // On signale la fin immédiate.
		return lookupSmall(n), nil
	}

	// Si le cas n'est pas simple, on délègue le travail au calculateur de cœur
	// qui contient la logique complexe (en O(log n)).
	return c.core.CalculateCore(ctx, progressChan, calcIndex, n, threshold)
}

// reportProgress effectue un envoi non bloquant sur le canal de progression.
func reportProgress(progressChan chan<- ProgressUpdate, calcIndex int, progress float64) {
	if progressChan == nil {
		return
	}
	update := ProgressUpdate{CalculatorIndex: calcIndex, Value: progress}

	// EXPLICATION ACADÉMIQUE : Envoi non-bloquant sur un canal
	// Un envoi normal `progressChan <- update` bloquerait si le canal est plein.
	// Dans un contexte de haute performance, on ne veut pas qu'un calcul soit ralenti
	// parce que l'affichage n'est pas assez rapide.
	// Le `select` avec une clause `default` permet un envoi non-bloquant :
	// - `case progressChan <- update`: Tente d'envoyer.
	// - `default`: Si l'envoi bloque (canal plein), exécute cette clause immédiatement.
	// On "perd" une mise à jour de progression, mais on ne ralentit pas le calcul.
	select {
	case progressChan <- update:
	default: // Le canal est plein ou non prêt, on ignore simplement la mise à jour.
	}
}

// --- OPTIMISATION : LOOKUP TABLE (LUT) ---

// fibLookupTable est un tableau qui stockera les 94 premiers nombres de Fibonacci.
var fibLookupTable [MaxFibUint64 + 1]*big.Int

// EXPLICATION ACADÉMIQUE : La fonction `init()`
// `init()` est une fonction spéciale en Go. S'il elle existe dans un package, elle
// est exécutée automatiquement une seule fois, lorsque le package est importé.
// C'est l'endroit idéal pour initialiser des états globaux, comme notre Lookup Table.
func init() {
	var a, b uint64 = 0, 1
	for i := uint64(0); i <= MaxFibUint64; i++ {
		fibLookupTable[i] = new(big.Int).SetUint64(a)
		a, b = b, a+b
	}
}

// lookupSmall récupère une valeur de la table.
func lookupSmall(n uint64) *big.Int {
	// EXPLICATION ACADÉMIQUE : Immuabilité et pointeurs
	// La LUT contient des pointeurs vers des `big.Int`. Si on retournait directement
	// `fibLookupTable[n]`, le code appelant pourrait modifier la valeur dans notre
	// table globale, ce qui est une source de bugs très difficiles à tracer.
	// En retournant une NOUVELLE copie (`new(big.Int).Set(...)`), on garantit
	// que la table de consultation reste immuable et que le programme est sûr.
	return new(big.Int).Set(fibLookupTable[n])
}

// --- OPTIMISATION : POOLING D'OBJETS (ZÉRO-ALLOCATION) ---

// EXPLICATION ACADÉMIQUE : `sync.Pool`
// `sync.Pool` est un outil de performance avancé pour réduire la pression sur le
// Garbage Collector (GC). Les calculs pour de grands `n` créent de nombreux
// objets `big.Int` temporaires. Allouer et libérer constamment ces objets force
// le GC à travailler beaucoup, ce qui peut causer des pauses dans l'application.
//
// Un `sync.Pool` est un "pool" d'objets réutilisables. Au lieu de créer un nouvel
// objet, on en demande un au pool (`Get`). Quand on a fini, on le remet dans le
// pool (`Put`). C'est beaucoup plus rapide que l'allocation mémoire.
//
// NOTE IMPORTANTE : Un pool n'est PAS un cache. Les objets dans le pool peuvent
// être supprimés à tout moment par le GC. C'est pourquoi il faut toujours fournir
// une fonction `New` pour créer un objet si le pool est vide.

// Structures et Pools pour Fast Doubling
type calculationState struct {
	f_k, f_k1      *big.Int // Les deux nombres de Fibonacci courants
	t1, t2, t3, t4 *big.Int // Entiers temporaires pour les calculs intermédiaires
}

var statePool = sync.Pool{
	// `New` est appelé par le pool quand `Get` est appelé sur un pool vide.
	New: func() interface{} {
		return &calculationState{
			f_k: new(big.Int), f_k1: new(big.Int),
			t1: new(big.Int), t2: new(big.Int),
			t3: new(big.Int), t4: new(big.Int),
		}
	},
}

// getState est une fonction d'assistance pour récupérer et réinitialiser un état.
func getState() *calculationState {
	// `statePool.Get()` récupère un objet du pool. Le type de retour est `interface{}`,
	// donc une assertion de type `.(*calculationState)` est nécessaire.
	s := statePool.Get().(*calculationState)

	// EXPLICATION CRUCIALE : Réinitialisation de l'état
	// Les objets retournés par le pool contiennent les données de leur dernière
	// utilisation. Il est absolument essentiel de les réinitialiser à un état
	// connu avant de les utiliser, pour éviter les bugs dus à des données "sales".
	s.f_k.SetInt64(0)
	s.f_k1.SetInt64(1)
	return s
}

// putState est une fonction d'assistance pour retourner un état au pool.
func putState(s *calculationState) {
	statePool.Put(s)
}

// --- Structures et Pools pour Matrix Exponentiation ---
// La même logique de pooling est appliquée ici pour l'algorithme matriciel.
type matrix struct {
	a, b, c, d *big.Int
}

func newMatrix() *matrix {
	return &matrix{new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
}

func (m *matrix) Set(other *matrix) {
	m.a.Set(other.a)
	m.b.Set(other.b)
	m.c.Set(other.c)
	m.d.Set(other.d)
}

type matrixState struct {
	res                            *matrix  // Matrice résultat
	p                              *matrix  // Matrice de puissance
	tempMatrix                     *matrix  // Matrice temporaire
	t1, t2, t3, t4, t5, t6, t7, t8 *big.Int // Entiers temporaires
}

var matrixStatePool = sync.Pool{
	New: func() interface{} {
		return &matrixState{
			res:        newMatrix(),
			p:          newMatrix(),
			tempMatrix: newMatrix(),
			t1:         new(big.Int), t2: new(big.Int), t3: new(big.Int), t4: new(big.Int),
			t5: new(big.Int), t6: new(big.Int), t7: new(big.Int), t8: new(big.Int),
		}
	},
}

func getMatrixState() *matrixState {
	s := matrixStatePool.Get().(*matrixState)
	// Réinitialisation de l'état des matrices pour un nouveau calcul.
	// res = Matrice Identité
	s.res.a.SetInt64(1)
	s.res.b.SetInt64(0)
	s.res.c.SetInt64(0)
	s.res.d.SetInt64(1)
	// p = Matrice de Fibonacci de base Q
	s.p.a.SetInt64(1)
	s.p.b.SetInt64(1)
	s.p.c.SetInt64(1)
	s.p.d.SetInt64(0)
	return s
}

func putMatrixState(s *matrixState) {
	matrixStatePool.Put(s)
}
