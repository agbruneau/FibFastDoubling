// EXPLICATION ACADÉMIQUE :
// Ce fichier implémente le calcul de Fibonacci via l'exponentiation matricielle,
// une autre méthode en O(log n). Elle est souvent plus facile à comprendre
// conceptuellement que le "Fast Doubling", mais peut être légèrement moins
// performante en pratique en raison d'un plus grand nombre de multiplications.
package fibonacci

import (
	"context"
	"math/big"
	"math/bits"
	"runtime"
	"sync"
)

// MatrixExponentiation est une implémentation de l'interface `coreCalculator`.
// EXPLICATION ACADÉMIQUE : L'algorithme d'Exponentiation Matricielle (O(log n))
//
// Cette méthode repose sur le fait que la suite de Fibonacci peut être exprimée
// par une transformation linéaire représentée par une matrice.
//
//  [ F(n+1) ] = [ 1  1 ] * [ F(n)   ]
//  [ F(n)   ]   [ 1  0 ]   [ F(n-1) ]
//
// En appliquant cette transformation `n` fois, on obtient :
//
//  [ F(n+1) ] = [ 1  1 ]^n * [ F(1) ]
//  [ F(n)   ]   [ 1  0 ]    [ F(0) ]
//
// Puisque F(1)=1 et F(0)=0, le calcul de F(n) se résume à calculer la matrice
// Q = [[1, 1], [1, 0]] élevée à la puissance `n`, et à prendre l'élément
// approprié. Le calcul de Q^n peut être fait très efficacement en O(log n)
// étapes en utilisant l'algorithme "d'exponentiation par carré".
type MatrixExponentiation struct{}

func (me *MatrixExponentiation) Name() string {
	return "MatrixExponentiation (SymmetricOpt+Parallel+ZeroAlloc+LUT)"
}

// squareSymmetricMatrix calcule le carré d'une matrice symétrique.
// EXPLICATION ACADÉMIQUE : Optimisation pour Matrice Symétrique
// Une multiplication matricielle standard de 2x2 (M * M) requiert 8 multiplications
// d'entiers. Cependant, si la matrice M est symétrique (l'élément [0,1] est égal
// à l'élément [1,0]), on peut optimiser le calcul.
//
// Soit M = [[a, b], [b, d]].
// M² = [[a²+b², ab+bd], [ab+bd, b²+d²]]
//
// On peut calculer tous les termes de M² avec seulement 4 multiplications
// d'entiers coûteuses : a², b², d², et b*(a+d).
// C'est une optimisation significative qui divise par deux le nombre de `big.Int.Mul`.
func squareSymmetricMatrix(dest, m *matrix, s *matrixState, useParallel bool, threshold int) {
	var wg sync.WaitGroup

	// Utilisation des entiers temporaires du pool d'état `s`.
	t_a_sq := s.t1
	t_b_sq := s.t2
	t_d_sq := s.t3
	t_b_ad := s.t4
	t_a_plus_d := s.t5

	t_a_plus_d.Add(m.a, m.d)

	// Comme pour Fast Doubling, on parallélise les multiplications indépendantes si
	// les nombres sont assez grands. Ici, il y en a 4.
	if useParallel && m.a.BitLen() > threshold {
		wg.Add(4)
		go func() { defer wg.Done(); t_a_sq.Mul(m.a, m.a) }()
		go func() { defer wg.Done(); t_b_sq.Mul(m.b, m.b) }()
		go func() { defer wg.Done(); t_d_sq.Mul(m.d, m.d) }()
		go func() { defer wg.Done(); t_b_ad.Mul(m.b, t_a_plus_d) }()
		wg.Wait()
	} else {
		// Exécution séquentielle pour les petits nombres.
		t_a_sq.Mul(m.a, m.a)
		t_b_sq.Mul(m.b, m.b)
		t_d_sq.Mul(m.d, m.d)
		t_b_ad.Mul(m.b, t_a_plus_d)
	}

	// Assemblage du résultat final.
	dest.a.Add(t_a_sq, t_b_sq)
	dest.b.Set(t_b_ad)
	dest.c.Set(t_b_ad) // La symétrie est préservée.
	dest.d.Add(t_b_sq, t_d_sq)
}

// multiplyMatrices effectue une multiplication de matrices 2x2 standard.
func multiplyMatrices(dest, m1, m2 *matrix, s *matrixState, useParallel bool, threshold int) {
	var wg sync.WaitGroup

	// Une multiplication de matrices 2x2 nécessite 8 multiplications d'entiers.
	// Ces 8 opérations sont toutes indépendantes et peuvent être parallélisées.
	if useParallel && m1.a.BitLen() > threshold {
		wg.Add(8)
		go func() { defer wg.Done(); s.t1.Mul(m1.a, m2.a) }()
		go func() { defer wg.Done(); s.t2.Mul(m1.b, m2.c) }()
		go func() { defer wg.Done(); s.t3.Mul(m1.a, m2.b) }()
		go func() { defer wg.Done(); s.t4.Mul(m1.b, m2.d) }()
		go func() { defer wg.Done(); s.t5.Mul(m1.c, m2.a) }()
		go func() { defer wg.Done(); s.t6.Mul(m1.d, m2.c) }()
		go func() { defer wg.Done(); s.t7.Mul(m1.c, m2.b) }()
		go func() { defer wg.Done(); s.t8.Mul(m1.d, m2.d) }()
		wg.Wait()
	} else {
		s.t1.Mul(m1.a, m2.a)
		s.t2.Mul(m1.b, m2.c)
		s.t3.Mul(m1.a, m2.b)
		s.t4.Mul(m1.b, m2.d)
		s.t5.Mul(m1.c, m2.a)
		s.t6.Mul(m1.d, m2.c)
		s.t7.Mul(m1.c, m2.b)
		s.t8.Mul(m1.d, m2.d)
	}

	// L'assemblage final (les additions) est fait séquentiellement.
	dest.a.Add(s.t1, s.t2)
	dest.b.Add(s.t3, s.t4)
	dest.c.Add(s.t5, s.t6)
	dest.d.Add(s.t7, s.t8)
}

// CalculateCore implémente l'algorithme d'exponentiation par carré.
func (me *MatrixExponentiation) CalculateCore(ctx context.Context, progressChan chan<- ProgressUpdate, calcIndex int, n uint64, threshold int) (*big.Int, error) {
	if n == 0 {
		return big.NewInt(0), nil
	}

	// Récupération de l'état (matrices et entiers temporaires) depuis le pool.
	s := getMatrixState()
	defer putMatrixState(s)

	k := n - 1 // On a besoin de Q^(n-1) pour trouver F(n).
	numBits := bits.Len64(k)
	invNumBits := 1.0
	if numBits > 0 {
		invNumBits = 1.0 / float64(numBits)
	}
	useParallel := runtime.NumCPU() > 1

	// `s.res` est la matrice résultat, initialisée à la matrice identité.
	// `s.p` est la matrice de base (Q), qui sera mise au carré à chaque étape.
	// `s.tempMatrix` est utilisée pour stocker les résultats intermédiaires.
	tempMatrix := s.tempMatrix

	// --- ALGORITHME D'EXPONENTIATION PAR CARRÉ (BINARY EXPONENTIATION) ---
	// On parcourt les bits de l'exposant `k` du moins significatif (LSB) au plus
	// significatif (MSB).
	for i := 0; i < numBits; i++ {
		// Vérification coopérative de l'annulation.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if progressChan != nil {
			reportProgress(progressChan, calcIndex, float64(i)*invNumBits)
		}

		// ÉTAPE 1: MULTIPLICATION CONDITIONNELLE
		// Si le i-ème bit de `k` est à 1, on multiplie notre résultat courant
		// par la puissance courante de la matrice de base.
		// res = res * p
		if (k>>uint(i))&1 == 1 {
			multiplyMatrices(tempMatrix, s.res, s.p, s, useParallel, threshold)
			s.res.Set(tempMatrix)
		}

		// ÉTAPE 2: MISE AU CARRÉ
		// On met la matrice de base au carré pour l'itération suivante.
		// p = p * p
		// Note : la matrice de base `p` reste symétrique tout au long du processus,
		// on peut donc utiliser la fonction optimisée.
		if i < numBits-1 { // Optimisation : on évite le dernier carré qui est inutile.
			squareSymmetricMatrix(tempMatrix, s.p, s, useParallel, threshold)
			s.p.Set(tempMatrix)
		}
	}

	reportProgress(progressChan, calcIndex, 1.0)
	// Le résultat F(n) se trouve dans l'élément en haut à gauche de la matrice Q^(n-1).
	// On retourne une copie pour garantir l'isolation par rapport au pool.
	return new(big.Int).Set(s.res.a), nil
}
