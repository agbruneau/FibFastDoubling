// EXPLICATION ACADÉMIQUE :
// Ce fichier implémente l'algorithme "Fast Doubling", optimisé pour la haute performance.
// Il combine une complexité algorithmique optimale O(log n) avec des optimisations
// système avancées : parallélisme au niveau des tâches (Task Parallelism) et gestion
// mémoire zéro-allocation.
package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// OptimizedFastDoubling est une implémentation de l'interface `coreCalculator`.
//
// EXPLICATION ACADÉMIQUE : L'algorithme Fast Doubling (O(log n))
// L'algorithme utilise des identités mathématiques pour "sauter" des étapes,
// permettant un calcul en temps logarithmique.
//
// Les identités clés sont :
// F(2k)   = F(k) * [2*F(k+1) - F(k)]
// F(2k+1) = F(k+1)² + F(k)²
//
// L'algorithme parcourt la représentation binaire de `n`. À chaque bit, il applique
// l'étape de "Doubling" (k -> 2k). Si le bit est 1, il applique une étape
// d'addition (2k -> 2k+1).
type OptimizedFastDoubling struct{}

// Name retourne le nom descriptif de l'algorithme et de ses optimisations.
func (fd *OptimizedFastDoubling) Name() string {
	return "Optimized Fast Doubling (O(log n) | 3-Way Parallel | Zero-Alloc)"
}

// CalculateCore implémente la logique principale de l'algorithme.
// [REFACTORING] Signature mise à jour pour correspondre à la nouvelle interface coreCalculator.
func (fd *OptimizedFastDoubling) CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error) {

	// --- INITIALISATION & GESTION MÉMOIRE ---

	// Étape 1 : Acquisition d'un état depuis le pool (Stratégie "Zéro-Allocation").
	// [REFACTORING] Utilisation des fonctions modernisées acquire/release.
	s := acquireState()
	// `defer releaseState(s)` garantit que l'état est retourné au pool à la fin,
	// même en cas d'erreur (idiome essentiel pour la gestion des ressources).
	defer releaseState(s)

	// `bits.Len64(n)` donne le nombre d'itérations nécessaires (la taille binaire de n).
	numBits := bits.Len64(n)

	// [BONIFICATION] Gestion robuste du cas limite n=0 (numBits=0).
	if numBits == 0 {
		// F(0) = 0. L'état initialisé a déjà f_k=0 (garanti par acquireState/Reset).
		return new(big.Int).Set(s.f_k), nil
	}

	// Pré-calcul de l'inverse pour optimiser le calcul de la progression dans la boucle.
	invNumBits := 1.0 / float64(numBits)

	// Le parallélisme n'est bénéfique que si on dispose de plusieurs cœurs CPU.
	useParallel := runtime.NumCPU() > 1

	// --- BOUCLE PRINCIPALE (O(log n) itérations) ---
	// Parcours des bits de `n` du plus significatif (MSB) au moins significatif (LSB).
	for i := numBits - 1; i >= 0; i-- {

		// --- GESTION DE L'ANNULATION (COOPERATIVE CANCELLATION) ---
		// EXPLICATION ACADÉMIQUE : Annulation Coopérative
		// On vérifie périodiquement le contexte pour permettre un arrêt propre.
		// NOTE IMPORTANTE : Les opérations de `math/big` (comme Mul) ne sont pas
		// interruptibles. Si une multiplication prend beaucoup de temps, l'annulation
		// ne sera détectée qu'après la fin de cette opération (par exemple, après wg.Wait()).
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Rapport de progression.
		// [REFACTORING] Utilisation de la fonction reporter découplée.
		if i < numBits-1 {
			// Calcul de la progression basée sur le nombre d'itérations terminées.
			reporter(float64(numBits-1-i) * invNumBits)
		}

		// --- ÉTAPE DE DOUBLING : Calcul de F(2k) et F(2k+1) ---

		// 1. Calcul du terme commun : t2 = 2*F(k+1) - F(k)
		s.t2.Lsh(s.f_k1, 1)   // t2 = f_k1 << 1 (multiplication par 2)
		s.t2.Sub(s.t2, s.f_k) // t2 = t2 - f_k

		// 2. Multiplications (Cœur de l'optimisation parallèle)
		// EXPLICATION ACADÉMIQUE : Seuil de Parallélisme
		// Activation conditionnelle basée sur le nombre de cœurs et la taille des nombres (threshold).
		// Pour les petits nombres, le coût de création/synchronisation des goroutines est supérieur au gain.
		if useParallel && s.f_k1.BitLen() > threshold {
			// [REFACTORING] Appel à la fonction d'assistance dédiée pour le parallélisme.
			parallelMultiply3(s)
		} else {
			// Exécution séquentielle si le parallélisme n'est pas activé.
			s.t3.Mul(s.f_k, s.t2)    // A: F(k) * t2
			s.t1.Mul(s.f_k1, s.f_k1) // B: F(k+1)²
			s.t4.Mul(s.f_k, s.f_k)   // C: F(k)²
		}

		// 3. Assemblage final des résultats.
		// F(2k) = t3
		// F(2k+1) = t1 + t4
		s.f_k.Set(s.t3)
		s.f_k1.Add(s.t1, s.t4)

		// --- ÉTAPE D'ADDITION CONDITIONNELLE ---
		// Si le i-ème bit de `n` est 1.
		if (n>>uint(i))&1 == 1 {
			// Passage de (F(k'), F(k'+1)) à (F(k'+1), F(k'+2)).
			// F(k'+2) = F(k'+1) + F(k').
			// Utilisation de t1 comme temporaire pour l'échange (swap).
			s.t1.Set(s.f_k1)
			s.f_k1.Add(s.f_k1, s.f_k)
			s.f_k.Set(s.t1)
		}
	}

	// La progression finale (1.0) est garantie par le décorateur (FibCalculator), pas ici.

	// EXPLICATION ACADÉMIQUE : Sécurité Mémoire et Pooling
	// CRUCIAL : `s.f_k` fait partie de l'objet `s` qui va être retourné au pool et réutilisé.
	// Il ne faut JAMAIS retourner un pointeur vers une mémoire recyclée.
	// On crée une NOUVELLE copie indépendante pour le résultat final.
	return new(big.Int).Set(s.f_k), nil
}

// [REFACTORING] Extraction de la logique de parallélisme.

// parallelMultiply3 exécute les trois multiplications indépendantes de l'étape de
// "doubling" en parallèle.
// Prérequis : s.t2 doit contenir [2*F(k+1) - F(k)].
func parallelMultiply3(s *calculationState) {
	// EXPLICATION ACADÉMIQUE : Parallélisme de Tâches (Task Parallelism)
	// Les trois multiplications (A, B, C) sont indépendantes et peuvent être exécutées simultanément.
	//
	// Stratégie "On-Demand" vs "Worker Pool" :
	// Nous créons 3 nouvelles goroutines à chaque itération. Bien que cela ait un coût,
	// il est souvent inférieur au coût de synchronisation (via canaux) nécessaire pour
	// utiliser un pool de workers persistant pour seulement 3 tâches fixes.
	// Le lancement "On-Demand" est ici considéré comme optimal.

	var wg sync.WaitGroup
	wg.Add(3) // On attend 3 tâches.

	// Goroutine A: t3 = F(k) * t2
	// EXPLICATION ACADÉMIQUE : Sécurité des Closures
	// On passe les pointeurs en arguments de la fonction anonyme (closure). C'est crucial
	// pour garantir que chaque goroutine travaille avec les bonnes données sans
	// risque de "race condition" ou de capture incorrecte des variables de l'état `s`.
	go func(dest, src1, src2 *big.Int) {
		defer wg.Done()
		dest.Mul(src1, src2)
	}(s.t3, s.f_k, s.t2)

	// Goroutine B: t1 = F(k+1)²
	go func(dest, src *big.Int) {
		defer wg.Done()
		// Note : Mul(x, x) est optimisé en interne pour le calcul du carré (squaring).
		dest.Mul(src, src)
	}(s.t1, s.f_k1)

	// Goroutine C: t4 = F(k)²
	go func(dest, src *big.Int) {
		defer wg.Done()
		dest.Mul(src, src)
	}(s.t4, s.f_k)

	// Synchronisation : Bloque jusqu'à ce que les 3 goroutines aient terminé.
	wg.Wait()
}
