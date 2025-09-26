// EXPLICATION ACADÉMIQUE :
// Ce fichier implémente le calcul de Fibonacci via l'Exponentiation Matricielle (O(log n)).
// Il illustre l'algorithme d'exponentiation binaire (par la mise au carré), l'optimisation
// mathématique (matrices symétriques), le parallélisme de tâches et la gestion mémoire zéro-allocation.
package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// MatrixExponentiation est une implémentation de l'interface `coreCalculator`.
//
// EXPLICATION ACADÉMIQUE : La Théorie de l'Exponentiation Matricielle
//
// La suite de Fibonacci est une transformation linéaire. La matrice Q = [[1, 1], [1, 0]]
// permet de passer d'un état au suivant :
//
//	[ F(k+1) ] = [ 1  1 ] * [  F(k)  ]
//	[  F(k)  ]   [ 1  0 ]   [ F(k-1) ]
//
// Par conséquent, pour calculer F(n), on peut utiliser la puissance (n-1) de Q :
//
//	[ F(n)  F(n-1) ] = Q^(n-1)
//	[F(n-1) F(n-2) ]
//
// F(n) est l'élément en haut à gauche de Q^(n-1).
// L'exponentiation binaire permet de calculer Q^k en O(log k) multiplications matricielles.
//
// Observation Clé : Symétrie
// Q est symétrique (Q = Qᵀ). Le carré d'une matrice symétrique est aussi symétrique.
// Toutes les puissances de Q sont donc symétriques, permettant une optimisation majeure.
type MatrixExponentiation struct{}

// Name retourne le nom descriptif de l'algorithme et de ses optimisations.
func (c *MatrixExponentiation) Name() string {
	return "Matrix Exponentiation (O(log n) | Symmetric Opt. | Parallel | Zero-Alloc)"
}

// CalculateCore implémente la logique principale de l'algorithme.
func (c *MatrixExponentiation) CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error) {
	// Cas de base F(0) = 0.
	if n == 0 {
		return big.NewInt(0), nil
	}

	// --- INITIALISATION & GESTION MÉMOIRE ---

	// Étape 1 : Acquisition d'un état complet depuis le pool (Zéro-Allocation).
	// L'état est réinitialisé par acquireMatrixState (via Reset()).
	state := acquireMatrixState()
	// `defer` garantit le retour de l'état au pool (gestion robuste des ressources).
	defer releaseMatrixState(state)

	// Pour F(n), nous calculons Q^(n-1).
	exponent := n - 1
	numBits := bits.Len64(exponent)

	// Pré-calcul pour le rapport de progression.
	var invNumBits float64
	if numBits > 0 {
		invNumBits = 1.0 / float64(numBits)
	}

	// Détection de la capacité de parallélisme.
	useParallel := runtime.NumCPU() > 1

	// --- ALGORITHME D'EXPONENTIATION BINAIRE (PAR LA MISE AU CARRÉ) ---
	// Parcours des bits de l'exposant (LSB vers MSB).
	// `res` (résultat) : Accumulateur (initialisé à Identité I).
	// `p` (puissance) : Puissances successives Q, Q², Q⁴... (initialisé à Q).
	for i := 0; i < numBits; i++ {
		// Vérification coopérative de l'annulation.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Rapport de progression.
		reporter(float64(i) * invNumBits)

		// ÉTAPE 1 : MULTIPLICATION CONDITIONNELLE (Si le i-ème bit est 1)
		// res = res * p
		if (exponent>>uint(i))&1 == 1 {
			multiplyMatrices(state.tempMatrix, state.res, state.p, state, useParallel, threshold)

			// OPTIMISATION MAJEURE : Échange de pointeurs (Pointer Swap)
			// Au lieu de copier le contenu de `tempMatrix` vers `res` (coûteux avec big.Int),
			// on échange les pointeurs. `res` pointe vers le nouveau résultat,
			// et l'ancien `res` devient le nouveau buffer temporaire.
			state.res, state.tempMatrix = state.tempMatrix, state.res
		}

		// ÉTAPE 2 : MISE AU CARRÉ
		// p = p * p (pour la prochaine itération)
		// OPTIMISATION : On évite la dernière mise au carré inutile.
		if i < numBits-1 {
			// Puisque `p` est garanti d'être symétrique, on utilise la fonction optimisée
			// (4 multiplications au lieu de 8).
			squareSymmetricMatrix(state.tempMatrix, state.p, state, useParallel, threshold)
			// Échange de pointeurs (Pointer Swap).
			state.p, state.tempMatrix = state.tempMatrix, state.p
		}
	}

	// Le résultat F(n) est l'élément (0,0) de la matrice résultat Q^(n-1).
	// CRUCIAL : On retourne une NOUVELLE copie. `state.res.a` appartient au pool et sera recyclé.
	return new(big.Int).Set(state.res.a), nil
}

// multiplyMatrices effectue une multiplication standard de deux matrices 2x2.
// C = A * B
func multiplyMatrices(dest, m1, m2 *matrix, state *matrixState, useParallel bool, threshold int) {
	// La multiplication standard requiert 8 multiplications d'entiers indépendantes.
	// C[0,0] = A[0,0]*B[0,0] + A[0,1]*B[1,0]  (t1 + t2)
	// C[0,1] = A[0,0]*B[0,1] + A[0,1]*B[1,1]  (t3 + t4)
	// C[1,0] = A[1,0]*B[0,0] + A[1,1]*B[1,0]  (t5 + t6)
	// C[1,1] = A[1,0]*B[0,1] + A[1,1]*B[1,1]  (t7 + t8)

	// Définition des tâches via closures.
	tasks := []func(){
		func() { state.t1.Mul(m1.a, m2.a) },
		func() { state.t2.Mul(m1.b, m2.c) },
		func() { state.t3.Mul(m1.a, m2.b) },
		func() { state.t4.Mul(m1.b, m2.d) },
		func() { state.t5.Mul(m1.c, m2.a) },
		func() { state.t6.Mul(m1.d, m2.c) },
		func() { state.t7.Mul(m1.c, m2.b) },
		func() { state.t8.Mul(m1.d, m2.d) },
	}

	// EXPLICATION ACADÉMIQUE : Seuil de Parallélisme Heuristique
	// On vérifie si la taille des nombres dépasse le seuil pour justifier le coût de synchronisation.
	// On utilise `m1.a` comme heuristique car c'est généralement l'élément le plus grand.
	shouldRunInParallel := useParallel && m1.a.BitLen() > threshold
	executeTasks(shouldRunInParallel, tasks)

	// L'assemblage final (additions) est rapide et fait séquentiellement.
	dest.a.Add(state.t1, state.t2)
	dest.b.Add(state.t3, state.t4)
	dest.c.Add(state.t5, state.t6)
	dest.d.Add(state.t7, state.t8)
}

// squareSymmetricMatrix calcule le carré d'une matrice symétrique de manière optimisée.
//
// EXPLICATION DE L'OPTIMISATION (Réduction de 50% des multiplications)
// Soit M une matrice symétrique : M = [[a, b], [b, d]].
//
// M² = [[a²+b², ab+bd],
//
//	[ab+bd, b²+d²]]
//
// M² = [[a²+b², b(a+d)],
//
//	[b(a+d), b²+d²]]
//
// Au lieu de 8 multiplications standard, on n'a besoin que de 4 calculs coûteux :
// a², b², d², et b*(a+d). C'est un gain de performance majeur pour les `big.Int`.
func squareSymmetricMatrix(dest, mat *matrix, state *matrixState, useParallel bool, threshold int) {
	// Alias pour les temporaires du pool.
	aSquared := state.t1
	bSquared := state.t2
	dSquared := state.t3
	bTimesAPlusD := state.t4
	aPlusD := state.t5 // Temporaire pour a+d

	// Calcul du terme commun (a+d).
	aPlusD.Add(mat.a, mat.d)

	// Définition des 4 tâches indépendantes.
	tasks := []func(){
		// Note : Mul(x, x) est optimisé en interne pour le calcul du carré (squaring).
		func() { aSquared.Mul(mat.a, mat.a) },
		func() { bSquared.Mul(mat.b, mat.b) },
		func() { dSquared.Mul(mat.d, mat.d) },
		func() { bTimesAPlusD.Mul(mat.b, aPlusD) },
	}

	// Exécution en parallèle si les conditions sont remplies.
	shouldRunInParallel := useParallel && mat.a.BitLen() > threshold
	executeTasks(shouldRunInParallel, tasks)

	// Assemblage de la matrice résultat.
	dest.a.Add(aSquared, bSquared)
	dest.b.Set(bTimesAPlusD)
	dest.c.Set(bTimesAPlusD) // La symétrie est préservée.
	dest.d.Add(bSquared, dSquared)
}

// executeTasks est une fonction utilitaire pour l'exécution parallèle de tâches indépendantes.
// Elle abstrait la logique de synchronisation via `sync.WaitGroup`.
func executeTasks(inParallel bool, tasks []func()) {
	if inParallel {
		var wg sync.WaitGroup
		wg.Add(len(tasks))

		// EXPLICATION ACADÉMIQUE : Stratégie de Parallélisme "On-Demand"
		// On lance de nouvelles goroutines pour chaque groupe de tâches. Pour un nombre
		// faible et fixe de tâches (ici 4 ou 8), le coût de lancement "à la demande"
		// est souvent inférieur au coût de synchronisation (via canaux) nécessaire
		// pour utiliser un pool de workers persistant.
		for _, task := range tasks {
			// EXPLICATION ACADÉMIQUE : Capture de variable de boucle (Idiome Go)
			// `task := task` crée une nouvelle variable locale ("shadowing") pour
			// garantir que chaque goroutine utilise la bonne fonction, évitant le bug
			// classique de concurrence (essentiel avant Go 1.22).
			task := task
			go func() {
				defer wg.Done()
				task()
			}()
		}
		// Synchronisation : Bloque jusqu'à ce que toutes les tâches soient terminées.
		wg.Wait()
	} else {
		// Exécution séquentielle.
		for _, task := range tasks {
			task()
		}
	}
}
