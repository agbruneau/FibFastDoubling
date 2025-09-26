//
// MODULE ACADÉMIQUE : ALGORITHME "FAST DOUBLING" OPTIMISÉ
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier implémente l'algorithme "Fast Doubling" pour le calcul de Fibonacci.
// Il est conçu comme une étude de cas pour la haute performance en Go, combinant :
//  1. Une complexité algorithmique optimale (O(log n)).
//  2. Une gestion de mémoire "zéro-allocation" dans la boucle critique via `sync.Pool`.
//  3. Le parallélisme de tâches ("Task Parallelism") pour exploiter les CPU multi-cœurs.
//  4. La gestion de l'annulation coopérative via le `context` de Go.
//
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
// EXPLICATION ACADÉMIQUE : Théorie de l'Algorithme "Fast Doubling" (O(log n))
//
// Cet algorithme est l'un des plus rapides connus pour le calcul de F(n). Il repose
// sur deux identités mathématiques qui permettent de "doubler" l'indice k en une seule étape :
//
//   F(2k)   = F(k) * [2*F(k+1) - F(k)]
//   F(2k+1) = F(k+1)² + F(k)²
//
// L'algorithme fonctionne en parcourant la représentation binaire de `n` du bit le plus
// significatif (MSB) vers le moins significatif (LSB). On maintient une paire de valeurs (F(k), F(k+1)).
// À chaque étape (pour chaque bit de `n`), on applique les formules ci-dessus pour passer
// de (F(k), F(k+1)) à (F(2k), F(2k+1)).
// Si le bit de `n` actuellement lu est 1, cela signifie que `n` a une composante impaire.
// On effectue alors une étape supplémentaire simple pour passer de (F(2k), F(2k+1))
// à (F(2k+1), F(2k+2)), en utilisant la relation F(m+1) = F(m) + F(m-1).
//
type OptimizedFastDoubling struct{}

// Name retourne le nom descriptif de l'algorithme et de ses optimisations.
func (fd *OptimizedFastDoubling) Name() string {
	return "Optimized Fast Doubling (O(log n) | Parallèle | Zéro-Alloc)"
}

// CalculateCore implémente la logique principale de l'algorithme.
func (fd *OptimizedFastDoubling) CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error) {

	// --- GESTION DE LA MÉMOIRE ET INITIALISATION ---
	// Acquisition d'un objet `calculationState` depuis le pool.
	// Cette seule allocation au début de la fonction évite d'en faire dans la boucle,
	// ce qui est la clé de la performance "zéro-allocation".
	s := acquireState()
	// `defer releaseState(s)` est une garantie absolue que l'objet sera retourné au pool.
	defer releaseState(s)

	// `bits.Len64(n)` est une manière très efficace de trouver la position du bit le plus
	// significatif, ce qui nous donne le nombre d'itérations nécessaires.
	numBits := bits.Len64(n)

	// Cas limite n=0 : F(0)=0. `acquireState` garantit que s.f_k est déjà à 0.
	if numBits == 0 {
		return new(big.Int).Set(s.f_k), nil
	}

	invNumBits := 1.0 / float64(numBits)
	useParallel := runtime.NumCPU() > 1

	// --- BOUCLE PRINCIPALE DE L'ALGORITHME (O(log n) itérations) ---
	// La boucle itère sur les bits de `n`, de gauche (MSB) à droite (LSB).
	for i := numBits - 1; i >= 0; i-- {

		// EXPLICATION ACADÉMIQUE : Annulation Coopérative ("Cooperative Cancellation")
		// Les calculs avec `math/big` peuvent être longs et ne sont pas nativement interruptibles.
		// Il est donc crucial de vérifier l'état du contexte (`ctx.Err()`) à chaque itération.
		// Si le contexte a été annulé (timeout, Ctrl+C), on arrête le calcul proprement.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Rapport de progression via le callback `reporter`.
		if i < numBits-1 {
			reporter(float64(numBits-1-i) * invNumBits)
		}

		// --- ÉTAPE DE DOUBLING : Calcul de (F(2k), F(2k+1)) à partir de (F(k), F(k+1)) ---
		// Les variables de l'état `s` (f_k, f_k1, t1, t2, t3, t4) sont réutilisées à chaque itération.

		// 1. Calcul du terme commun : t2 = 2*F(k+1) - F(k)
		s.t2.Lsh(s.f_k1, 1)   // t2 = f_k1 * 2
		s.t2.Sub(s.t2, s.f_k) // t2 = t2 - f_k

		// 2. Calcul des trois multiplications coûteuses.
		//    F(2k)   = F(k) * (2*F(k+1) - F(k)) -> s.t3 = s.f_k * s.t2
		//    F(2k+1) = F(k+1)² + F(k)²          -> s.f_k1 = s.t1 + s.t4
		//
		// EXPLICATION ACADÉMIQUE : Seuil de Parallélisme
		// Le parallélisme a un coût (création de goroutines, synchronisation).
		// Pour des nombres "petits", ce coût est plus élevé que le gain de temps obtenu.
		// On n'active donc le parallélisme que si le nombre de bits des opérandes
		// dépasse un `threshold` configurable, et si le système a plus d'un cœur.
		if useParallel && s.f_k1.BitLen() > threshold {
			parallelMultiply3(s)
		} else {
			s.t3.Mul(s.f_k, s.t2)    // F(k) * t2
			s.t1.Mul(s.f_k1, s.f_k1) // F(k+1)²
			s.t4.Mul(s.f_k, s.f_k)   // F(k)²
		}

		// 3. Assemblage des résultats du doubling.
		// La nouvelle valeur de F(k) est F(2k) = s.t3
		s.f_k.Set(s.t3)
		// La nouvelle valeur de F(k+1) est F(2k+1) = s.t1 + s.t4
		s.f_k1.Add(s.t1, s.t4)

		// --- ÉTAPE D'ADDITION CONDITIONNELLE ---
		// Si le i-ème bit de `n` est à 1, on doit avancer d'un pas.
		if (n>>uint(i))&1 == 1 {
			// On passe de (F(2k), F(2k+1)) à (F(2k+1), F(2k+2))
			// en utilisant F(2k+2) = F(2k+1) + F(2k).
			// On utilise t1 comme variable temporaire pour effectuer l'échange.
			s.t1.Set(s.f_k1)          // t1 = F(2k+1)
			s.f_k1.Add(s.f_k1, s.f_k) // f_k1 = F(2k+1) + F(2k) = F(2k+2)
			s.f_k.Set(s.t1)           // f_k = t1 = F(2k+1)
		}
	}

	// EXPLICATION ACADÉMIQUE : SÉCURITÉ MÉMOIRE ET POOLING
	// C'est le point le plus critique de l'utilisation de `sync.Pool`.
	// L'objet `s` et tous ses champs (y compris `s.f_k`) vont être retournés au pool
	// via `defer releaseState(s)`. Si on retournait `s.f_k` directement, l'appelant
	// recevrait un pointeur vers une mémoire qui pourrait être réutilisée et modifiée
	// par une autre goroutine à tout moment.
	// Il est donc IMPÉRATIF de retourner une NOUVELLE copie du résultat final.
	return new(big.Int).Set(s.f_k), nil
}

// parallelMultiply3 exécute les trois multiplications indépendantes de l'étape de "doubling"
// en parallèle.
// Pré-requis : s.t2 doit déjà contenir la valeur `2*F(k+1) - F(k)`.
func parallelMultiply3(s *calculationState) {
	// EXPLICATION ACADÉMIQUE : Parallélisme de Tâches ("Task Parallelism")
	// Les trois multiplications sont mathématiquement indépendantes :
	//   A = F(k) * t2
	//   B = F(k+1) * F(k+1)
	//   C = F(k) * F(k)
	// Elles peuvent donc être exécutées simultanément, chacune dans sa propre goroutine.
	// `sync.WaitGroup` est l'outil de synchronisation parfait pour ce cas : on attend
	// que les N tâches soient terminées avant de continuer.

	var wg sync.WaitGroup
	wg.Add(3)

	// Tâche A: s.t3 = s.f_k * s.t2
	go func() {
		defer wg.Done()
		s.t3.Mul(s.f_k, s.t2)
	}()

	// Tâche B: s.t1 = s.f_k1 * s.f_k1
	go func() {
		defer wg.Done()
		s.t1.Mul(s.f_k1, s.f_k1) // Mul est optimisé pour le carré (squaring).
	}()

	// Tâche C: s.t4 = s.f_k * s.f_k
	go func() {
		defer wg.Done()
		s.t4.Mul(s.f_k, s.f_k)
	}()

	// Bloque l'exécution jusqu'à ce que `wg.Done()` ait été appelé 3 fois.
	// Après cette ligne, on a la garantie que s.t1, s.t3, et s.t4 contiennent les bons résultats.
	wg.Wait()
}