//
// MODULE ACADÉMIQUE : ARCHITECTURE DU CALCULATEUR ET OPTIMISATIONS
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier est le cœur architectural du module `fibonacci`. Il ne contient pas d'algorithme
// de calcul principal, mais définit les "règles du jeu" et fournit des optimisations
// fondamentales. Il illustre des concepts avancés d'ingénierie logicielle :
//  1. CONTRATS ET ABSTRACTIONS : Définition des interfaces (`Calculator`, `coreCalculator`)
//     qui découplent l'orchestrateur des implémentations d'algorithmes (Principe de
//     Ségrégation des Interfaces).
//  2. PATRONS DE CONCEPTION : Application pratique des patrons Décorateur et Adaptateur
//     pour ajouter des fonctionnalités et adapter les interfaces de manière modulaire.
//  3. OPTIMISATION MÉMOIRE ("ZÉRO-ALLOCATION") : Utilisation de `sync.Pool` pour créer
//     des pools d'objets réutilisables, réduisant drastiquement la pression sur le
//     Garbage Collector (GC) de Go, une technique essentielle pour la haute performance.
//  4. SÉCURITÉ DE TYPAGE AVEC LES GÉNÉRIQUES : Utilisation des génériques de Go (1.18+)
//     pour créer des wrappers de pool typés, éliminant les assertions de type risquées.
//  5. GESTION D'ÉTAT ET IMMUABILITÉ : Démonstration des bonnes pratiques pour gérer
//     l'état des objets mis en pool et garantir l'immuabilité des données partagées (LUT).
//
package fibonacci

import (
	"context"
	"math/big"
	"sync"
)

const (
	// MaxFibUint64 correspond à F(93), le dernier nombre de Fibonacci qui peut être
	// stocké dans un `uint64`. C'est la limite pour l'optimisation O(1) via la table de consultation (LUT).
	MaxFibUint64 = 93

	// DefaultParallelThreshold (en bits) est le seuil au-delà duquel les multiplications
	// en parallèle sont activées. En dessous, le coût de synchronisation des goroutines
	// est supérieur au gain de performance du calcul parallèle.
	DefaultParallelThreshold = 2048
)

// ProgressUpdate est la structure de données (DTO) utilisée pour communiquer l'état
// de la progression d'un calcul via un canal, du producteur (calculateur) au consommateur (UI).
type ProgressUpdate struct {
	CalculatorIndex int     // Identifie quel calculateur (en mode parallèle) a envoyé la mise à jour.
	Value           float64 // Progression normalisée entre 0.0 et 1.0.
}

// ProgressReporter définit un type fonctionnel (callback) simple pour rapporter la progression.
// En fournissant cette abstraction aux algorithmes de cœur, on les découple complètement
// du mécanisme de communication (ici, les canaux). Ils n'ont besoin de savoir que "comment
// rapporter", pas "à qui ou via quel moyen".
type ProgressReporter func(progress float64)

// === PATRONS DE CONCEPTION : INTERFACE, DÉCORATEUR & ADAPTATEUR ===

// Calculator est l'interface PUBLIQUE du module. C'est le seul point d'entrée que
// l'orchestrateur (`main.go`) connaît.
type Calculator interface {
	Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// coreCalculator est une interface INTERNE, non exportée. Elle représente le contrat
// pour un algorithme de calcul "pur". Elle est volontairement simple et ne dépend pas
// des détails d'orchestration comme les canaux ou les index.
// C'est un exemple du Principe de Ségrégation des Interfaces (le 'I' de SOLID).
type coreCalculator interface {
	CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error)
	Name() string
}

// FibCalculator est une structure qui implémente l'interface `Calculator`.
// EXPLICATION ACADÉMIQUE : Application des Patrons Décorateur et Adaptateur
// Cette structure est une implémentation élégante de deux patrons de conception :
//  1. DÉCORATEUR : `FibCalculator` "enveloppe" un `coreCalculator`. Il ajoute des
//     fonctionnalités communes (appelées "cross-cutting concerns") comme l'optimisation
//     via la table de consultation (LUT) et la garantie que la progression atteigne 100%.
//     Cela évite de dupliquer cette logique dans chaque algorithme.
//  2. ADAPTATEUR : Sa méthode `Calculate` adapte l'interface attendue par l'orchestrateur
//     (avec un `chan<- ProgressUpdate`) à l'interface plus simple requise par le
//     `coreCalculator` (un `ProgressReporter`).
type FibCalculator struct {
	core coreCalculator
}

// NewCalculator est une "factory function" qui construit le décorateur autour d'un noyau.
func NewCalculator(core coreCalculator) Calculator {
	if core == nil {
		panic("fibonacci: NewCalculator a reçu un coreCalculator nil")
	}
	return &FibCalculator{core: core}
}

// Name délègue simplement l'appel à l'objet enveloppé.
func (c *FibCalculator) Name() string {
	return c.core.Name()
}

// Calculate est la méthode centrale qui orchestre les rôles de décorateur et d'adaptateur.
func (c *FibCalculator) Calculate(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {

	// --- Rôle d'ADAPTATEUR : Création du ProgressReporter ---
	// La closure `reporter` capture les détails d'implémentation (`progressChan`, `calcIndex`)
	// et expose une fonction simple `func(float64)`.
	reporter := func(progress float64) {
		if progressChan == nil {
			return
		}
		if progress > 1.0 { // Bornage de sécurité.
			progress = 1.0
		}
		update := ProgressUpdate{CalculatorIndex: calcIndex, Value: progress}

		// EXPLICATION ACADÉMIQUE : Envoi Non-Bloquant sur un Canal
		// Le `select` avec une clause `default` permet un envoi non-bloquant.
		// DÉCISION DE CONCEPTION : On priorise la performance du calcul. Si le canal
		// de progression est plein (parce que l'UI est lente à le consommer),
		// on préfère abandonner cette mise à jour de progression (`default`) plutôt
		// que de bloquer la goroutine de calcul en attendant que l'UI soit prête.
		select {
		case progressChan <- update: // Tente d'envoyer.
		default: // Abandonne si le canal n'est pas immédiatement prêt.
		}
	}

	// --- Rôle de DÉCORATEUR : Optimisation "Fast Path" (O(1)) ---
	// Avant de lancer un calcul coûteux, on vérifie si le résultat est déjà connu.
	if n <= MaxFibUint64 {
		reporter(1.0) // Le calcul est "instantané", on signale 100% de progression.
		return lookupSmall(n), nil
	}

	// Délégation à l'algorithme de cœur pour les cas complexes.
	result, err := c.core.CalculateCore(ctx, reporter, n, threshold)

	// Filet de sécurité du décorateur : s'assurer que 100% est bien rapporté en cas de succès.
	if err == nil && result != nil {
		reporter(1.0)
	}

	return result, err
}

// --- OPTIMISATION : TABLE DE CONSULTATION (LOOKUP TABLE, LUT) ---
var fibLookupTable [MaxFibUint64 + 1]*big.Int

// `init` est utilisée pour pré-calculer les petites valeurs de Fibonacci au démarrage.
func init() {
	var a, b uint64 = 0, 1
	for i := uint64(0); i <= MaxFibUint64; i++ {
		fibLookupTable[i] = new(big.Int).SetUint64(a)
		a, b = b, a+b
	}
}

// lookupSmall récupère une valeur de la LUT.
func lookupSmall(n uint64) *big.Int {
	// EXPLICATION ACADÉMIQUE : Garantie d'Immuabilité
	// CRUCIAL : On retourne une NOUVELLE copie (`new(big.Int).Set(...)`) de la valeur.
	// `big.Int` est un type pointeur. Si on retournait `fibLookupTable[n]` directement,
	// le code appelant pourrait modifier la valeur dans la table globale partagée,
	// ce qui est une source de bugs extrêmement difficiles à tracer. Cette copie garantit
	// que la LUT reste immuable.
	return new(big.Int).Set(fibLookupTable[n])
}

// === OPTIMISATION AVANCÉE : POOLING D'OBJETS ("ZÉRO-ALLOCATION") ===

// EXPLICATION ACADÉMIQUE : Le Problème du Garbage Collector (GC)
// Les calculs avec `math/big` génèrent un grand nombre d'objets intermédiaires. Chaque
// multiplication ou addition crée de nouveaux `big.Int`. Pour des calculs intensifs,
// cela met une pression énorme sur le GC, qui doit constamment scanner et nettoyer la
// mémoire, provoquant des pauses qui dégradent les performances.
//
// SOLUTION : `sync.Pool`
// `sync.Pool` est un cache d'objets temporaires géré par le runtime. En réutilisant
// des objets (`Get` et `Put`), on évite les cycles allocation/libération, ce qui
// réduit la charge du GC et peut amener à des performances "zéro-allocation"
// dans les boucles de calcul critiques.

// Pool[T] est un wrapper générique et typé autour de `sync.Pool`.
// L'utilisation des génériques (Go 1.18+) apporte la sécurité de type, évitant les
// assertions de type manuelles (`x.(T)`) qui sont source d'erreurs à l'exécution.
type Pool[T any] struct {
	pool sync.Pool
}

func NewPool[T any](newFunc func() T) *Pool[T] {
	return &Pool[T]{
		pool: sync.Pool{
			New: func() interface{} { return newFunc() },
		},
	}
}
func (p *Pool[T]) Get() T { return p.pool.Get().(T) }
func (p *Pool[T]) Put(x T) { p.pool.Put(x) }

// Resettable définit un contrat pour les objets qui peuvent être mis en pool.
// Un objet doit pouvoir être réinitialisé à un état propre avant d'être réutilisé.
type Resettable interface {
	Reset()
}

// acquireFromPool récupère un objet du pool et garantit sa réinitialisation.
func acquireFromPool[T Resettable](p *Pool[T]) T {
	item := p.Get()
	// EXPLICATION CRUCIALE : Les objets d'un pool sont "sales" !
	// Ils contiennent les données de leur dernière utilisation. Il est absolument
	// obligatoire de les réinitialiser avant toute nouvelle utilisation pour éviter
	// la corruption de données.
	item.Reset()
	return item
}

// releaseToPool retourne un objet au pool.
func releaseToPool[T any](p *Pool[T], item T) {
	// Vérification pour éviter de mettre un pointeur `nil` dans le pool.
	if v, ok := (interface{})(item).(*big.Int); ok && v == nil { return }
	if v, ok := (interface{})(item).(*calculationState); ok && v == nil { return }
	if v, ok := (interface{})(item).(*matrixState); ok && v == nil { return }
	p.Put(item)
}


// --- DÉFINITION DES OBJETS MIS EN POOL ET DE LEURS POOLS ---

// calculationState regroupe toutes les variables temporaires pour l'algorithme Fast Doubling.
// Mettre en pool la structure entière est plus simple et efficace que de gérer des pools
// pour chaque `big.Int` individuellement.
type calculationState struct {
	f_k, f_k1, t1, t2, t3, t4 *big.Int
}

// Reset implémente l'interface `Resettable`.
func (s *calculationState) Reset() {
	s.f_k.SetInt64(0)  // F(k)   -> F(0) = 0
	s.f_k1.SetInt64(1) // F(k+1) -> F(1) = 1
	// OPTIMISATION : Les temporaires (t1-t4) n'ont pas besoin d'être remis à zéro car
	// ils sont toujours la destination d'une opération (`.Mul`, `.Add`) qui écrase
	// complètement leur contenu avant toute lecture.
}

var statePool = NewPool(func() *calculationState {
	return &calculationState{
		f_k: new(big.Int), f_k1: new(big.Int), t1: new(big.Int),
		t2: new(big.Int), t3: new(big.Int), t4: new(big.Int),
	}
})

func acquireState() *calculationState { return acquireFromPool(statePool) }
func releaseState(s *calculationState) { releaseToPool(statePool, s) }


// matrix représente une matrice 2x2.
type matrix struct { a, b, c, d *big.Int }

func newMatrix() *matrix {
	return &matrix{new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
}
func (m *matrix) Set(other *matrix) {
	m.a.Set(other.a); m.b.Set(other.b); m.c.Set(other.c); m.d.Set(other.d)
}
func (m *matrix) SetIdentity() {
	m.a.SetInt64(1); m.b.SetInt64(0); m.c.SetInt64(0); m.d.SetInt64(1)
}
func (m *matrix) SetBaseQ() {
	m.a.SetInt64(1); m.b.SetInt64(1); m.c.SetInt64(1); m.d.SetInt64(0)
}

// matrixState regroupe toutes les variables pour l'algorithme d'exponentiation matricielle.
type matrixState struct {
	res, p, tempMatrix             *matrix
	t1, t2, t3, t4, t5, t6, t7, t8 *big.Int
}

// Reset implémente l'interface `Resettable`.
func (s *matrixState) Reset() {
	s.res.SetIdentity() // L'accumulateur de résultat commence à la matrice identité.
	s.p.SetBaseQ()      // La matrice de puissance commence à la matrice de base Q.
}

var matrixStatePool = NewPool(func() *matrixState {
	return &matrixState{
		res: newMatrix(), p: newMatrix(), tempMatrix: newMatrix(),
		t1: new(big.Int), t2: new(big.Int), t3: new(big.Int), t4: new(big.Int),
		t5: new(big.Int), t6: new(big.Int), t7: new(big.Int), t8: new(big.Int),
	}
})

func acquireMatrixState() *matrixState { return acquireFromPool(matrixStatePool) }
func releaseMatrixState(s *matrixState) { releaseToPool(matrixStatePool, s) }