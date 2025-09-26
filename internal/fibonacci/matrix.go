//
// MODULE ACADÉMIQUE : ALGORITHME D'EXPONENTIATION MATRICIELLE
//
// OBJECTIF PÉDAGOGIQUE :
// Ce fichier implémente le calcul de la suite de Fibonacci via l'exponentiation
// matricielle, une méthode classique de complexité O(log n). Il sert d'exemple pour :
//  1. La mise en œuvre de l'algorithme "d'exponentiation par la mise au carré".
//  2. L'optimisation d'opérations mathématiques (multiplication de matrices symétriques).
//  3. Le parallélisme de tâches pour les calculs intensifs sur les grands nombres.
//  4. L'intégration avec un système de gestion de mémoire "zéro-allocation" via des pools d'objets.
//
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
// EXPLICATION ACADÉMIQUE : La Théorie de l'Exponentiation Matricielle (O(log n))
//
// Cette méthode repose sur une propriété fondamentale de la suite de Fibonacci : elle peut
// être décrite par une transformation linéaire, représentée par une matrice 2x2.
//
// La relation de récurrence F(n) = F(n-1) + F(n-2) peut s'écrire sous forme matricielle :
//
//	[ F(n+1) ] = [ 1  1 ] * [  F(n)  ]
//	[  F(n)  ]   [ 1  0 ]   [ F(n-1) ]
//
// En appliquant cette transformation de manière répétée, on obtient :
//
//	[ F(n+1) ] = [ 1  1 ]^n * [ F(1) ]
//	[  F(n)  ]   [ 1  0 ]    [ F(0) ]
//
// Puisque F(1)=1 et F(0)=0, le vecteur initial est [1, 0]. Le calcul de F(n) se ramène
// donc au calcul de la puissance n-1 de la "matrice de Fibonacci" Q = [[1, 1], [1, 0]].
// Le résultat F(n) est alors l'élément en haut à gauche de la matrice Q^(n-1).
//
// L'étape clé est que le calcul de Q^k peut être effectué très efficacement en O(log k)
// opérations matricielles grâce à l'algorithme "d'exponentiation par la mise au carré"
// (ou exponentiation binaire), au lieu de k-1 multiplications naïves.
//
type MatrixExponentiation struct{}

// Name retourne le nom descriptif de l'algorithme et de ses optimisations.
func (c *MatrixExponentiation) Name() string {
	return "Exponentiation Matricielle (Opt. Symétrique+Parallèle+Zéro-Alloc)"
}

// CalculateCore implémente la logique principale de l'algorithme.
func (c *MatrixExponentiation) CalculateCore(ctx context.Context, reporter ProgressReporter, n uint64, threshold int) (*big.Int, error) {
	// Cas de base F(0) = 0.
	if n == 0 {
		return big.NewInt(0), nil
	}

	// Étape 1 : Acquisition d'un état complet depuis le pool (stratégie "Zéro-Allocation").
	// Cet état contient toutes les matrices et les entiers temporaires nécessaires, pré-alloués.
	state := acquireMatrixState()
	// `defer` garantit que l'état est retourné au pool, même en cas d'erreur. C'est crucial.
	defer releaseMatrixState(state)

	// Pour trouver F(n), nous avons besoin de calculer Q^(n-1).
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
	// La boucle parcourt les bits de l'exposant, du moins significatif (LSB) au plus significatif (MSB).
	// Deux matrices de l'état sont utilisées :
	// - `res` (résultat) : Accumule le résultat final. Initialisée à la matrice Identité.
	// - `p` (puissance) : Contient les puissances successives de Q (Q, Q², Q⁴, Q⁸, ...). Initialisée à Q.
	for i := 0; i < numBits; i++ {
		// Vérification coopérative de l'annulation (timeout, Ctrl+C).
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Rapport de progression.
		reporter(float64(i) * invNumBits)

		// ÉTAPE 1 : MULTIPLICATION CONDITIONNELLE
		// Si le i-ème bit de l'exposant est à 1, on doit multiplier notre résultat
		// par la puissance actuelle de la matrice de base : res = res * p.
		if (exponent>>uint(i))&1 == 1 {
			multiplyMatrices(state.tempMatrix, state.res, state.p, state, useParallel, threshold)
			// OPTIMISATION : Échange de pointeurs.
			// Au lieu de copier le contenu de `tempMatrix` dans `res` (ce qui serait coûteux),
			// on échange simplement les pointeurs. `res` pointe maintenant vers le nouveau résultat,
			// et l'ancienne matrice `res` devient le nouveau buffer temporaire.
			state.res, state.tempMatrix = state.tempMatrix, state.res
		}

		// ÉTAPE 2 : MISE AU CARRÉ
		// On met la matrice de puissance au carré pour la prochaine itération : p = p * p.
		// Cette opération est effectuée à chaque tour, que le bit soit 1 ou 0.
		if i < numBits-1 { // Optimisation : on évite la dernière mise au carré, qui est inutile.
			// Fait crucial : la matrice de base Q est symétrique. Le carré d'une matrice
			// symétrique est aussi symétrique. `p` reste donc toujours symétrique.
			// Nous pouvons donc utiliser une fonction de mise au carré optimisée.
			squareSymmetricMatrix(state.tempMatrix, state.p, state, useParallel, threshold)
			// On échange à nouveau les pointeurs pour la même raison d'efficacité.
			state.p, state.tempMatrix = state.tempMatrix, state.p
		}
	}

	// Le résultat F(n) se trouve dans l'élément (0,0) de la matrice résultat finale.
	// CRUCIAL : On retourne une NOUVELLE copie. Le pointeur `state.res.a` appartient
	// à l'objet du pool qui va être recyclé. Le retourner directement mènerait à une
	// corruption de mémoire ("use after free").
	return new(big.Int).Set(state.res.a), nil
}

// multiplyMatrices effectue une multiplication standard de deux matrices 2x2.
// C = A * B
func multiplyMatrices(dest, m1, m2 *matrix, state *matrixState, useParallel bool, threshold int) {
	// Une multiplication de matrices 2x2 standard requiert 8 multiplications d'entiers
	// et 4 additions.
	// C[0,0] = A[0,0]*B[0,0] + A[0,1]*B[1,0]
	// C[0,1] = A[0,0]*B[0,1] + A[0,1]*B[1,1]
	// C[1,0] = A[1,0]*B[0,0] + A[1,1]*B[1,0]
	// C[1,1] = A[1,0]*B[0,1] + A[1,1]*B[1,1]
	//
	// Les 8 multiplications sont indépendantes les unes des autres et peuvent donc
	// être parallélisées.
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

	// On n'active le parallélisme que si le CPU a plusieurs cœurs et si les nombres
	// sont assez grands (dépolacement le `threshold`) pour justifier le coût de
	// création et de synchronisation des goroutines.
	shouldRunInParallel := useParallel && m1.a.BitLen() > threshold
	executeTasks(shouldRunInParallel, tasks)

	// L'assemblage final (les additions) est fait séquentiellement.
	dest.a.Add(state.t1, state.t2)
	dest.b.Add(state.t3, state.t4)
	dest.c.Add(state.t5, state.t6)
	dest.d.Add(state.t7, state.t8)
}

// squareSymmetricMatrix calcule le carré d'une matrice symétrique de manière optimisée.
//
// EXPLICATION DE L'OPTIMISATION :
// Soit M une matrice symétrique : M = [[a, b], [b, d]].
// Le calcul standard de M² = M * M requerrait 8 multiplications d'entiers.
//
// Cependant, en développant M², on obtient :
// M² = [[a²+b², ab+bd], [ab+bd, b²+d²]]
//    = [[a²+b², b(a+d)], [b(a+d), b²+d²]]
//
// On constate que les termes ne dépendent que de 4 calculs coûteux :
// a², b², d², et b*(a+d).
// Cette optimisation divise par deux le nombre de multiplications de `big.Int`,
// ce qui représente un gain de performance considérable.
func squareSymmetricMatrix(dest, mat *matrix, state *matrixState, useParallel bool, threshold int) {
	// Utilisation des entiers temporaires du pool.
	aSquared := state.t1
	bSquared := state.t2
	dSquared := state.t3
	bTimesAPlusD := state.t4
	aPlusD := state.t5 // Temporaire pour a+d

	// Calcul des 4 termes indépendants.
	aPlusD.Add(mat.a, mat.d)
	tasks := []func(){
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

// executeTasks est une fonction utilitaire qui exécute un ensemble de tâches (closures).
// Elle abstrait la logique de parallélisation via un `sync.WaitGroup`.
func executeTasks(inParallel bool, tasks []func()) {
	if inParallel {
		var wg sync.WaitGroup
		wg.Add(len(tasks))
		for _, task := range tasks {
			// Lancement de chaque tâche dans une goroutine distincte.
			go func(f func()) {
				defer wg.Done() // wg.Done() est appelé à la fin de la tâche.
				f()
			}(task)
		}
		// wg.Wait() bloque jusqu'à ce que toutes les goroutines aient appelé wg.Done().
		wg.Wait()
	} else {
		// Si le parallélisme n'est pas activé, exécute les tâches séquentiellement.
		for _, task := range tasks {
			task()
		}
	}
}