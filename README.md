# Calculateur de Fibonacci Haute Performance (Go)

Ce projet est une implémentation de référence pour le calcul des nombres de la suite de Fibonacci (F(n)) pour des indices `n` arbitrairement grands, en utilisant Go pur.

Il démontre des techniques avancées d'ingénierie de performance et d'architecture concurrente robuste.

## Caractéristiques Clés

1.  **Algorithmes O(log n) :** Implémentation de "Optimized Fast Doubling" (le plus rapide en pratique) et "Matrix Exponentiation".
2.  **Parallélisme au Niveau des Opérations (Task Parallelism) :** Les multiplications indépendantes de `big.Int` sont exécutées simultanément sur plusieurs cœurs CPU via des goroutines.
3.  **Gestion Mémoire "Zéro-Allocation" :** Utilisation intensive de `sync.Pool` pour recycler les structures `big.Int`, minimisant la pression sur le Garbage Collector (GC).
4.  **Concurrence Structurée :** Utilisation de `golang.org/x/sync/errgroup` et `context` pour une orchestration robuste, garantissant une terminaison propre (Graceful Shutdown) lors des timeouts ou des annulations (Ctrl+C).
5.  **Optimisations Supplémentaires :**
    *   Lookup Table (LUT) O(1) pour n <= 93.
    *   Optimisation de la symétrie matricielle (réduction de 8 à 4 multiplications lors de la mise au carré).

## Prérequis

*   [Go](https://golang.org/dl/) (Version 1.21 ou supérieure recommandée)

## Installation et Compilation

1. Clonez le dépôt :
   ```bash
   git clone [https://github.com/VOTRE_USERNAME/fibcalc-hp.git](https://github.com/VOTRE_USERNAME/fibcalc-hp.git)
   cd fibcalc-hp