// EXPLICATION ACADÉMIQUE :
// Ce fichier implémente l'algorithme "Fast Doubling", qui est l'un des moyens les
// plus rapides connus pour calculer les nombres de Fibonacci. Il illustre des
// concepts de performance très avancés : l'optimisation algorithmique, le
// parallélisme au niveau des tâches et la gestion de la mémoire.
package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// OptimizedFastDoubling est une implémentation de l'interface `coreCalculator`.
// EXPLICATION ACADÉMIQUE : L'algorithme Fast Doubling (O(log n))
//
// L'approche itérative classique pour Fibonacci (F(n) = F(n-1) + F(n-2)) a une
// complexité temporelle de O(n). Pour de très grands `n`, c'est trop lent.
// L'algorithme "Fast Doubling" utilise une paire d'identités pour calculer F(n)
// en seulement O(log n) étapes, ce qui est exponentiellement plus rapide.
//
// Les identités sont :
// F(2k)   = F(k) * [2*F(k+1) - F(k)]
// F(2k+1) = F(k+1)² + F(k)²
//
// Comment ça marche ?
// L'algorithme fonctionne en considérant la représentation binaire de `n`. Il
// commence par le bit de poids le plus fort (MSB) et parcourt les bits vers la
// droite.
// À chaque étape, il calcule (F(2k), F(2k+1)) à partir de (F(k), F(k+1)) (l'étape "Doubling").
// Si le bit courant de `n` est 1, il applique une étape supplémentaire pour passer
// de (F(2k), F(2k+1)) à (F(2k+1), F(2k+2)) (l'étape "Addition").
// En parcourant tous les bits de `n`, on arrive au résultat final F(n).
type OptimizedFastDoubling struct{}

// Name retourne le nom de l'algorithme, y compris les optimisations clés.
func (fd *OptimizedFastDoubling) Name() string {
	return "OptimizedFastDoubling (3-Way-Parallel+ZeroAlloc+LUT)"
}

// CalculateCore implémente la logique principale de l'algorithme.
func (fd *OptimizedFastDoubling) CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	// Étape 1 : Récupération d'un état depuis le pool d'objets.
	// C'est le cœur de la stratégie "zéro-allocation".
	s := getState()
	// `defer putState(s)` garantit que l'état est retourné au pool à la fin de la
	// fonction, qu'elle se termine normalement ou par une erreur. C'est un idiome
	// Go très courant pour la gestion des ressources.
	defer putState(s)

	// `bits.Len64(n)` donne le nombre de bits nécessaires pour représenter `n`,
	// ce qui correspond au nombre d'itérations de la boucle principale.
	numBits := bits.Len64(n)
	invNumBits := 1.0 / float64(numBits) // Pré-calcul pour le rapport de progression.

	var wg sync.WaitGroup
	// Le parallélisme n'est utile que s'il y a plus d'un cœur CPU disponible.
	useParallel := runtime.NumCPU() > 1

	// Boucle principale : Parcours des bits de `n` du plus significatif (MSB)
	// au moins significatif (LSB).
	for i := numBits - 1; i >= 0; i-- {

		// --- GESTION DE L'ANNULATION (COOPERATIVE CANCELLATION) ---
		// EXPLICATION ACADÉMIQUE : Annulation Coopérative
		// Une goroutine ne peut pas être arrêtée de force de l'extérieur. Elle doit
		// coopérer en vérifiant périodiquement si une annulation a été demandée.
		// En vérifiant `ctx.Err() != nil` au début de chaque itération (qui peut être
		// longue), on s'assure que le calcul s'arrêtera rapidement si le contexte
		// est annulé (par timeout ou par un signal de l'utilisateur).
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Rapporte la progression (si le canal existe).
		if progressChan != nil && i < numBits-1 {
			reportProgress(progressChan, calcIndex, float64(numBits-1-i)*invNumBits)
		}

		// --- ÉTAPE DE DOUBLING : Calcul de F(2k) et F(2k+1) ---
		// On calcule les nouveaux (F_k, F_k+1) qui correspondront à F(2k) et F(2k+1).
		// Les variables s.f_k et s.f_k1 sont réutilisées et écrasées.

		// 1. Calcul du terme commun `2*F(k+1) - F(k)` (stocké dans s.t2)
		s.t2.Lsh(s.f_k1, 1)   // s.t2 = s.f_k1 << 1  (i.e., 2*F(k+1))
		s.t2.Sub(s.t2, s.f_k) // s.t2 = s.t2 - s.f_k

		// --- OPTIMISATION : PARALLÉLISME DE TÂCHES (TASK PARALLELISM) ---
		// EXPLICATION ACADÉMIQUE :
		// L'étape de "doubling" nécessite trois multiplications coûteuses :
		//  - `F(k) * (2*F(k+1) - F(k))`  (pour calculer F(2k))
		//  - `F(k+1)²`                     (partie de F(2k+1))
		//  - `F(k)²`                       (partie de F(2k+1))
		// Ces trois opérations sont mathématiquement indépendantes ! On peut donc les
		// exécuter en parallèle sur différents cœurs CPU pour accélérer le calcul.

		// On active le parallélisme seulement si `useParallel` est vrai et si la
		// taille des nombres (`BitLen`) dépasse le `threshold`.
		if useParallel && s.f_k1.BitLen() > threshold {
			// `wg.Add(3)` indique au WaitGroup que nous allons attendre 3 goroutines.
			wg.Add(3)

			// Goroutine A: t3 = F(k) * (2*F(k+1) - F(k))
			go func(dest, src1, src2 *big.Int) {
				defer wg.Done() // Signale la fin de cette goroutine.
				dest.Mul(src1, src2)
			}(s.t3, s.f_k, s.t2) // Les arguments sont passés pour éviter les problèmes de capture de variables de boucle.

			// Goroutine B: t1 = F(k+1)²
			go func(dest, src *big.Int) {
				defer wg.Done()
				dest.Mul(src, src)
			}(s.t1, s.f_k1)

			// Goroutine C: t4 = F(k)²
			go func(dest, src *big.Int) {
				defer wg.Done()
				dest.Mul(src, src)
			}(s.t4, s.f_k)

			// `wg.Wait()` bloque l'exécution jusqu'à ce que `wg.Done()` ait été
			// appelé 3 fois. Cela garantit que les 3 multiplications sont terminées
			// avant de continuer.
			wg.Wait()

		} else {
			// Si le parallélisme n'est pas activé, on exécute les multiplications
			// de manière séquentielle.
			s.t3.Mul(s.f_k, s.t2)
			s.t1.Mul(s.f_k1, s.f_k1)
			s.t4.Mul(s.f_k, s.f_k)
		}

		// Assemblage final des résultats des multiplications.
		// F(2k) = s.t3
		// F(2k+1) = s.t1 + s.t4
		s.f_k.Set(s.t3)
		s.f_k1.Add(s.t1, s.t4)

		// --- ÉTAPE D'ADDITION CONDITIONNELLE ---
		// Si le i-ème bit de `n` est 1, on doit passer de (F(2k), F(2k+1))
		// à (F(2k+1), F(2k+2)).
		// F(2k+2) = F(2k+1) + F(2k).
		if (n>>uint(i))&1 == 1 {
			// La nouvelle paire (f_k, f_k+1) devient (f_k+1, f_k + f_k+1).
			// On utilise s.t1 comme variable temporaire pour effectuer l'échange.
			s.t1.Set(s.f_k1)          // t1 = f_k1
			s.f_k1.Add(s.f_k1, s.f_k) // f_k1 = f_k1 + f_k
			s.f_k.Set(s.t1)           // f_k = t1
		}
	}

	reportProgress(progressChan, calcIndex, 1.0)
	// EXPLICATION ACADÉMIQUE : Retourner une copie
	// `s.f_k` est un pointeur vers une valeur qui fait partie de l'objet `s` qui
	// va être retourné au `sync.Pool`. Il ne faut JAMAIS retourner un pointeur
	// vers une mémoire qui va être recyclée.
	// On crée donc une nouvelle copie `new(big.Int).Set(s.f_k)` pour le résultat final,
	// garantissant que l'appelant a sa propre mémoire, indépendante du pool.
	return new(big.Int).Set(s.f_k), nil
}
