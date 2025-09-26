# FibCalc : Calculateur Fibonacci de Haute Performance en Go

## Introduction

**FibCalc** est une application en ligne de commande (CLI) écrite en Go pour calculer les nombres de la suite de Fibonacci pour de très grands indices (`n`). Au-delà de sa fonction première, ce projet est avant tout un **outil pédagogique** et une **étude de cas** illustrant des concepts avancés d'ingénierie logicielle, de performance et de concurrence en Go.

Le code est abondamment commenté pour servir de guide, expliquant les décisions architecturales, les patrons de conception (`design patterns`) et les optimisations mises en œuvre. Ce README sert de synthèse et de point d'entrée pour l'exploration du code source.

## Fonctionnalités

*   **Calcul de Très Grands Nombres :** Utilise `math/big` pour une précision arbitraire, permettant de calculer F(n) pour des `n` se chiffrant en millions.
*   **Algorithmes Performants :** Implémentation de deux algorithmes de complexité **O(log n)** :
    *   **Fast Doubling**
    *   **Exponentiation Matricielle**
*   **Mode Benchmark et Validation :** Exécute tous les algorithmes en parallèle, compare leurs temps d'exécution et valide que leurs résultats sont identiques.
*   **Parallélisme Optimisé :** Exploite les processeurs multi-cœurs pour accélérer les multiplications coûteuses de grands nombres.
*   **Optimisation Mémoire "Zéro-Allocation" :** Utilise des pools d'objets (`sync.Pool`) pour minimiser la pression sur le Garbage Collector (GC) et atteindre des performances maximales.
*   **Arrêt Propre (Graceful Shutdown) :** Gère les signaux du système (ex: `Ctrl+C`) pour s'arrêter proprement sans laisser de goroutines orphelines.
*   **Mode de Calibration Automatique :** Trouve et recommande le meilleur réglage de performance pour le parallélisme sur la machine de l'utilisateur.
*   **Interface en Ligne de Commande Robuste :** Validation des arguments, messages d'erreur clairs et codes de sortie standardisés.

## Concepts Académiques et d'Ingénierie

Ce répertoire est une démonstration pratique des principes suivants.

### 1. Architecture Logicielle (Software Architecture)

*   **Séparation des Préoccupations (Separation of Concerns) :** La logique est clairement découpée :
    *   `cmd/fibcalc/main.go` : Le point d'entrée ("Composition Root") qui assemble les dépendances et gère le cycle de vie de l'application.
    *   `internal/fibonacci/` : Le cœur applicatif, contenant la logique métier pure et les algorithmes.
    *   `internal/cli/` : La gestion de l'interface utilisateur (affichage de la progression).
*   **Principe Ouvert/Fermé (Open/Closed Principle) :** Grâce au patron "Registry" dans `main.go`, de nouveaux algorithmes peuvent être ajoutés sans modifier le code d'orchestration existant.
*   **Injection de Dépendances (Dependency Injection) :** Les fonctions clés comme `run` reçoivent leurs dépendances (contexte, configuration) en paramètre, ce qui les rend testables et découplées de l'état global.
*   **Patrons Décorateur et Adaptateur (`Decorator`, `Adapter`) :** Le `FibCalculator` dans `internal/fibonacci/calculator.go` "décore" les algorithmes de base avec des fonctionnalités communes (cache pour les petites valeurs, etc.) et "adapte" une interface de canal à une simple fonction de rappel (`callback`).

### 2. Concurrence et Parallélisme (Concurrency and Parallelism)

*   **Concurrence Structurée :** Utilisation de `golang.org/x/sync/errgroup` pour gérer des groupes de goroutines. Cela garantit qu'une erreur dans une goroutine annule les autres et que toutes les erreurs sont correctement propagées.
*   **Parallélisme de Tâches (Task Parallelism) :** Les multiplications coûteuses dans les algorithmes sont parallélisées (`fastdoubling.go`, `matrix.go`). La stratégie est optimisée pour réduire l'overhead en exécutant une partie du travail sur la goroutine appelante.
*   **Sécurité de Concurrence (Thread Safety) :** Les techniques utilisées sont intrinsèquement sûres :
    *   Communication par canaux pour les mises à jour de progression.
    *   Absence de mémoire partagée modifiable entre les goroutines de calcul (chacune écrit dans un index de slice distinct).
*   **Arrêt Coopératif (Cooperative Cancellation) :** Les boucles de calcul intensif vérifient périodiquement l'état du `context` (`ctx.Err()`) pour répondre aux signaux d'annulation (timeout, `Ctrl+C`).

### 3. Optimisation de la Performance

*   **Complexité Algorithmique O(log n) :** Utilisation d'algorithmes exponentiels, essentiels pour les grands `n`.
*   **Gestion Mémoire "Zéro-Allocation" :** L'utilisation intensive de `sync.Pool` dans `calculator.go` pour recycler les objets `big.Int` et les structures d'état (`calculationState`, `matrixState`) est la pierre angulaire de la performance. Elle évite des millions d'allocations mémoire et réduit la charge sur le GC.
*   **Optimisations Mathématiques :** L'algorithme d'exponentiation matricielle exploite la symétrie des matrices pour réduire de moitié le nombre de multiplications d'entiers nécessaires à chaque mise au carré (`matrix.go`).
*   **Optimisation par Lookup Table (LUT) :** Les 94 premiers nombres de Fibonacci sont pré-calculés et stockés, offrant une réponse en O(1) pour les petites valeurs de `n`.

### 4. Qualité et Robustesse du Code

*   **Immuabilité :** La LUT retourne des *copies* des `big.Int` pour empêcher la modification accidentelle de l'état global partagé. De même, les algorithmes retournent des copies des résultats finaux avant de libérer les objets dans le pool.
*   **Testabilité :** Le code est structuré pour être testé unitairement. Par exemple, le parsing des arguments est isolé dans une fonction pure qui ne dépend pas de `os.Args`.
*   **Gestion Robuste des Erreurs :** Utilisation de `errors.Is` pour inspecter les chaînes d'erreurs et distinguer les erreurs de contexte (timeout, annulation) des autres erreurs.
*   **Codes de Sortie Standards :** L'application utilise des codes de sortie (`exit codes`) pour communiquer son état final, permettant son intégration facile dans des scripts automatisés.

## Installation

Pour utiliser ce projet, assurez-vous d'avoir [Go](https://go.dev/doc/install) (version 1.18 ou supérieure) installé.

1.  Clonez le répertoire :
    ```bash
    git clone <URL_DU_REPO>
    cd <NOM_DU_REPO>
    ```

2.  Compilez l'application :
    ```bash
    go build -o fibcalc ./cmd/fibcalc
    ```
    Cela créera un exécutable nommé `fibcalc` dans le répertoire courant.

## Utilisation

L'exécutable `fibcalc` peut être utilisé avec plusieurs options.

**Syntaxe de base :**

```bash
./fibcalc [options]
```

**Options disponibles :**

| Flag          | Description                                                                                              | Défaut        |
|---------------|----------------------------------------------------------------------------------------------------------|---------------|
| `-n`          | L'indice 'n' de la suite de Fibonacci à calculer.                                                        | `250000000`   |
| `-algo`       | L'algorithme à utiliser. Options : `fast`, `matrix`, ou `all` pour comparer.                             | `all`         |
| `-timeout`    | Délai maximum d'exécution (ex: `10s`, `5m`, `1h`).                                                       | `5m`          |
| `-threshold`  | Seuil (en bits) pour activer la multiplication parallèle dans les algorithmes.                           | `2048`        |
| `--calibrate` | Lance le mode de calibration pour trouver le meilleur seuil pour votre machine. Ignore les autres calculs. | `false`       |
| `-v`, `-verbose`| Affiche le résultat complet du nombre de Fibonacci (peut être très long). Par défaut, il est tronqué.    | `false`       |
| `-h`, `-help` | Affiche l'aide.                                                                                          |               |

### Exemples

1.  **Exécuter une comparaison des algorithmes pour n = 10,000,000 :**
    ```bash
    ./fibcalc -n 10000000 -algo all
    ```

2.  **Calculer F(50,000,000) en utilisant l'algorithme "Fast Doubling" avec un timeout de 30 secondes :**
    ```bash
    ./fibcalc -n 50000000 -algo fast -timeout 30s
    ```

3.  **Calculer F(1,000,000) avec l'algorithme matriciel et afficher le résultat complet :**
    ```bash
    ./fibcalc -n 1000000 -algo matrix -v
    ```

4.  **Trouver le meilleur réglage de performance pour votre machine :**
    ```bash
    ./fibcalc --calibrate
    ```

## Licence

Ce projet est sous licence [LICENSE](./LICENSE).