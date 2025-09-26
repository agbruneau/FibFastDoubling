// EXPLICATION ACADÉMIQUE :
// Ce fichier est le cœur architectural du projet. Il définit les contrats (interfaces),
// implémente les optimisations fondamentales (LUT, Object Pooling) et utilise des patrons
// de conception avancés (Décorateur, Adaptateur) ainsi que les Generics pour la sécurité de typage.
package fibonacci

import (
	"context"
	"math/big"
	"sync"
)

const (
	// MaxFibUint64 est F(93). C'est le dernier indice dont le résultat tient dans un uint64.
	// Limite pour l'optimisation par Lookup Table (O(1)).
	MaxFibUint64 = 93

	// DefaultParallelThreshold (en bits) définit le seuil pour activer la multiplication
	// parallèle. En dessous, le coût de synchronisation des goroutines dépasse le gain.
	DefaultParallelThreshold = 2048
)

// ProgressUpdate est la structure communiquée via canal pour l'affichage.
type ProgressUpdate struct {
	CalculatorIndex int     // Identifiant du calculateur (pour le mode parallèle)
	Value           float64 // Progression de 0.0 à 1.0
}

// [REFACTORING MAJEUR] : Ségrégation des Interfaces

// ProgressReporter est une fonction de rappel (callback) fournie aux algorithmes
// de calcul pour signaler leur progression (0.0 à 1.0) sans connaître les détails
// d'implémentation de l'affichage (canaux, index).
type ProgressReporter func(progress float64)

// === PATRONS DE CONCEPTION : INTERFACE, DÉCORATEUR & ADAPTATEUR ===

// Calculator définit l'interface publique (API) pour tous les calculateurs.
type Calculator interface {
	// Calculate orchestre le calcul. La signature publique reste inchangée pour la compatibilité.
	Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// coreCalculator est une interface interne représentant un algorithme "pur".
type coreCalculator interface {
	// CalculateCore effectue le calcul mathématique. Elle est agnostique des détails d'orchestration.
	// [REFACTORING] Signature modifiée pour utiliser ProgressReporter.
	CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// FibCalculator implémente l'interface `Calculator`.
// EXPLICATION ACADÉMIQUE : Patrons Décorateur et Adaptateur
//  1. DÉCORATEUR : Il enveloppe un `coreCalculator` et ajoute des fonctionnalités
//     transversales (ici, l'optimisation "fast path" avec la LUT).
//  2. ADAPTATEUR : Il adapte l'interface abstraite `ProgressReporter` (attendue par le cœur)
//     à l'interface concrète `chan<- ProgressUpdate` (utilisée par l'orchestrateur).
type FibCalculator struct {
	core coreCalculator
}

// NewCalculator est une "factory function" qui crée et configure le décorateur.
func NewCalculator(core coreCalculator) Calculator {
	if core == nil {
		// Panic est approprié ici car c'est une erreur de programmation (Fail-Fast).
		panic("fibonacci: NewCalculator a reçu un coreCalculator nil")
	}
	return &FibCalculator{core: core}
}

// Name délègue l'appel à l'objet enveloppé.
func (c *FibCalculator) Name() string {
	return c.core.Name()
}

// Calculate est le cœur du décorateur et de l'adaptateur.
func (c *FibCalculator) Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {

	// --- Rôle d'ADAPTATEUR : Création du ProgressReporter concret ---
	// On crée une closure qui capture `calcIndex` et `progressChan` et implémente la logique non bloquante.
	reporter := func(progress float64) {
		if progressChan == nil {
			return
		}

		// Assurer que la progression est bornée à 1.0.
		if progress > 1.0 {
			progress = 1.0
		}

		update := ProgressUpdate{CalculatorIndex: calcIndex, Value: progress}

		// EXPLICATION ACADÉMIQUE : Envoi non-bloquant (`select` avec `default`)
		// On priorise la vitesse du calcul sur la garantie de l'affichage.
		// Si le canal est plein (l'affichage est lent), on abandonne la mise à jour
		// (`default`) au lieu de bloquer le calcul.
		select {
		case progressChan <- update:
		default:
			// Le canal est plein ou non prêt. On ignore cette mise à jour.
		}
	}

	// --- Rôle de DÉCORATEUR : Optimisation "Fast Path" (O(1)) ---
	if n <= MaxFibUint64 {
		reporter(1.0) // On signale la fin immédiate.
		return lookupSmall(n), nil
	}

	// Si le cas est complexe (O(log n)), on délègue au calculateur de cœur.
	result, err := c.core.CalculateCore(ctx, reporter, n, threshold)

	// Safety Net : Garantir que 100% est rapporté en cas de succès.
	if err == nil && result != nil {
		reporter(1.0)
	}

	return result, err
}

// --- OPTIMISATION : LOOKUP TABLE (LUT) PRÉCALCULÉE ---

var fibLookupTable [MaxFibUint64 + 1]*big.Int

// init() est exécutée automatiquement à l'importation du package.
func init() {
	var a, b uint64 = 0, 1
	for i := uint64(0); i <= MaxFibUint64; i++ {
		fibLookupTable[i] = new(big.Int).SetUint64(a)
		a, b = b, a+b
	}
}

// lookupSmall récupère une valeur de la table de manière sécurisée.
func lookupSmall(n uint64) *big.Int {
	// EXPLICATION ACADÉMIQUE : Immuabilité et Sécurité
	// CRUCIAL : On retourne une NOUVELLE copie (`new(big.Int).Set(...)`).
	// Si on retournait directement le pointeur de la table, l'appelant pourrait
	// modifier la table globale. Cette copie garantit l'immuabilité de la LUT.
	return new(big.Int).Set(fibLookupTable[n])
}

// === OPTIMISATION AVANCÉE : POOLING D'OBJETS (ZÉRO-ALLOCATION) ===

// EXPLICATION ACADÉMIQUE : `sync.Pool` et Garbage Collector (GC)
// `sync.Pool` permet de réutiliser les objets temporaires (`big.Int`) au lieu de les
// allouer/libérer constamment, réduisant drastiquement la pression sur le GC.

// [REFACTORING MAJEUR] : Utilisation de Generics (Go 1.18+) pour un Pool sécurisé.

// Pool[T] est un wrapper générique autour de sync.Pool pour une sécurité de typage.
type Pool[T any] struct {
	pool sync.Pool
}

// NewPool crée un nouveau Pool typé avec une fonction de création.
func NewPool[T any](newFunc func() T) *Pool[T] {
	return &Pool[T]{
		pool: sync.Pool{
			// L'assertion de type est gérée ici lors de la création.
			New: func() interface{} { return newFunc() },
		},
	}
}

// Get récupère un élément du pool de manière typée.
func (p *Pool[T]) Get() T {
	// L'assertion de type est encapsulée ici lors de la récupération.
	return p.pool.Get().(T)
}

// Put remet un élément dans le pool.
func (p *Pool[T]) Put(x T) {
	p.pool.Put(x)
}

// [REFACTORING MAJEUR] : Abstraction de la réinitialisation.

// Resettable définit un objet qui peut être réinitialisé à un état initial propre.
type Resettable interface {
	Reset()
}

// acquireFromPool récupère un objet du pool et garantit qu'il est réinitialisé.
func acquireFromPool[T Resettable](p *Pool[T]) T {
	item := p.Get()
	// EXPLICATION CRUCIALE : Réinitialisation de l'état
	// Les objets du pool contiennent des données "sales". La réinitialisation est obligatoire.
	item.Reset()
	return item
}

// releaseToPool retourne un objet au pool.
func releaseToPool[T any](p *Pool[T], item T) {
	// [BONIFICATION] S'assurer qu'on ne met pas un objet nil (si T est un type pointeur) dans le pool.
	if interface{}(item) != nil {
		p.Put(item)
	}
}

// === Structures et Pools pour Fast Doubling ===

// calculationState regroupe les variables pour l'algorithme Fast Doubling.
type calculationState struct {
	f_k, f_k1      *big.Int // F(k) et F(k+1)
	t1, t2, t3, t4 *big.Int // Temporaires
}

// Implémentation de Resettable.
func (s *calculationState) Reset() {
	// Réinitialisation à l'état initial : F(0)=0, F(1)=1.
	s.f_k.SetInt64(0)
	s.f_k1.SetInt64(1)
	// OPTIMISATION : Les temporaires (t1-t4) n'ont pas besoin d'être réinitialisés car
	// ils seront systématiquement écrasés (.Mul(), .Add(), etc.) avant d'être lus.
}

// Initialisation du pool générique.
var statePool = NewPool(func() *calculationState {
	return &calculationState{
		f_k: new(big.Int), f_k1: new(big.Int),
		t1: new(big.Int), t2: new(big.Int),
		t3: new(big.Int), t4: new(big.Int),
	}
})

// Fonctions d'assistance simplifiées grâce aux Generics et à Resettable.
// Renommage sémantique : acquire/release.
func acquireState() *calculationState {
	return acquireFromPool(statePool)
}

func releaseState(s *calculationState) {
	releaseToPool(statePool, s)
}

// === Structures et Pools pour Matrix Exponentiation ===

// matrix représente une matrice 2x2 [[a, b], [c, d]] de big.Int.
type matrix struct {
	a, b, c, d *big.Int
}

func newMatrix() *matrix {
	return &matrix{new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
}

// Set copie les valeurs d'une autre matrice.
func (m *matrix) Set(other *matrix) {
	m.a.Set(other.a)
	m.b.Set(other.b)
	m.c.Set(other.c)
	m.d.Set(other.d)
}

// SetIdentity configure la matrice en Identité I = [[1, 0], [0, 1]].
func (m *matrix) SetIdentity() {
	m.a.SetInt64(1)
	m.b.SetInt64(0)
	m.c.SetInt64(0)
	m.d.SetInt64(1)
}

// SetBaseQ configure la matrice de transition Q = [[1, 1], [1, 0]].
func (m *matrix) SetBaseQ() {
	m.a.SetInt64(1)
	m.b.SetInt64(1)
	m.c.SetInt64(1)
	m.d.SetInt64(0)
}

// matrixState regroupe les variables pour l'algorithme Matrix Exponentiation.
type matrixState struct {
	res        *matrix // Matrice résultat (accumulateur)
	p          *matrix // Matrice de puissance (power)
	tempMatrix *matrix // Matrice temporaire
	// Temporaires pour les calculs intermédiaires (8 pour multiplication 2x2 standard).
	t1, t2, t3, t4, t5, t6, t7, t8 *big.Int
}

// Implémentation de Resettable.
func (s *matrixState) Reset() {
	// Réinitialisation pour l'exponentiation binaire :
	s.res.SetIdentity() // Accumulateur commence à I.
	s.p.SetBaseQ()      // Puissance commence à Q.
	// Les temporaires (tempMatrix, t1-t8) n'ont pas besoin d'être réinitialisés.
}

// Initialisation du pool générique.
var matrixStatePool = NewPool(func() *matrixState {
	return &matrixState{
		res:        newMatrix(),
		p:          newMatrix(),
		tempMatrix: newMatrix(),
		t1:         new(big.Int), t2: new(big.Int), t3: new(big.Int), t4: new(big.Int),
		t5: new(big.Int), t6: new(big.Int), t7: new(big.Int), t8: new(big.Int),
	}
})

// Fonctions d'assistance simplifiées.
func acquireMatrixState() *matrixState {
	return acquireFromPool(matrixStatePool)
}

func releaseMatrixState(s *matrixState) {
	releaseToPool(matrixStatePool, s)
}
