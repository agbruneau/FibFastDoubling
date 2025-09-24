# Calculateur de Fibonacci Haute Performance en Go

Ce projet est une application en ligne de commande (CLI) écrite en Go pour calculer les nombres de la suite de Fibonacci (F(n)) pour des indices `n` arbitrairement grands. Il est conçu comme une démonstration de techniques d'ingénierie logicielle avancées, notamment en matière de performance, de concurrence et d'architecture logicielle robuste.

## Caractéristiques Clés

*   **Algorithmes Performants :** Implémentation de deux algorithmes `O(log n)` : "Fast Doubling" et "Matrix Exponentiation".
*   **Concurrence Structurée :** Utilisation de `golang.org/x/sync/errgroup` pour une gestion robuste des goroutines, garantissant une annulation propre et une propagation rapide des erreurs.
*   **Gestion des Signaux et Timeouts :** Intégration complète de `context` pour gérer les timeouts et les signaux du système d'exploitation (Ctrl+C), assurant un arrêt gracieux ("graceful shutdown").
*   **Optimisation Mémoire :** Utilisation de `sync.Pool` pour le recyclage des objets `big.Int`, réduisant la pression sur le Garbage Collector (GC) dans les calculs intensifs.
*   **Validation Croisée :** Un mode "benchmark" qui exécute tous les algorithmes en parallèle et valide que leurs résultats sont identiques.
*   **Code Pédagogique :** Le code source est abondamment commenté pour expliquer les choix d'architecture et les idiomes Go utilisés.

## Prérequis

*   [Go](https://golang.org/dl/) (version 1.20 ou supérieure)

## Installation

1.  Clonez le dépôt :
    ```bash
    git clone https://github.com/votre-nom-utilisateur/fibcalc-hp.git
    cd fibcalc-hp
    ```
    *(Remplacez `votre-nom-utilisateur` par votre nom d'utilisateur GitHub si vous forkez le projet).*

## Utilisation

L'exécutable peut être lancé directement avec `go run` ou après avoir compilé le binaire.

### Syntaxe de base

```bash
go run ./cmd/fibcalc/main.go [options]
```

### Options (Flags)

| Flag          | Description                                                                 | Valeur par défaut |
|---------------|-----------------------------------------------------------------------------|-------------------|
| `-n`          | L'indice 'n' de la séquence de Fibonacci à calculer.                        | `100000000`       |
| `-algo`       | Algorithme à utiliser : `fast`, `matrix`, ou `all` pour comparer.           | `all`             |
| `-v`          | Mode verbeux : affiche le nombre de Fibonacci complet.                      | `false`           |
| `-timeout`    | Délai maximum pour le calcul (ex: `10s`, `1m30s`).                          | `5m`              |
| `-threshold`  | Seuil (en nombre de bits) pour activer la multiplication parallèle.         | `2048`            |

### Exemples

1.  **Calcul simple avec l'algorithme "fast" :**
    ```bash
    go run ./cmd/fibcalc/main.go -n 100000 -algo fast
    ```

2.  **Comparer les deux algorithmes pour F(200,000,000) et afficher le résultat complet :**
    ```bash
    go run ./cmd/fibcalc/main.go -n 200000000 -algo all -v
    ```

3.  **Lancer un calcul avec un timeout de 30 secondes :**
    ```bash
    go run ./cmd/fibcalc/main.go -n 500000000 -timeout 30s
    ```

## Algorithmes Implémentés

*   **Fast Doubling (`fast`) :** Un algorithme `O(log n)` très efficace en pratique, basé sur les identités `F(2k)` et `F(2k+1)`.
*   **Matrix Exponentiation (`matrix`) :** Une approche classique `O(log n)` qui utilise la puissance de la matrice de Fibonacci `[[1, 1], [1, 0]]` pour calculer F(n).

## Notes d'Architecture

*   **Découplage :** Le code est organisé en paquets `internal` pour une encapsulation claire. La logique métier (`internal/fibonacci`) est séparée de l'interface utilisateur (`internal/cli`) et du point d'entrée (`cmd/fibcalc`).
*   **Extensibilité :** Les algorithmes sont enregistrés via un "Registry Pattern", ce qui permet d'en ajouter de nouveaux sans modifier le code principal.
*   **Testabilité :** La logique principale est contenue dans la fonction `run()`, qui accepte ses dépendances (contexte, configuration, writer), la rendant facile à tester unitairement, contrairement à `main()` qui gère les effets de bord.

## Compiler et Tester

### Compiler le binaire

Pour créer un exécutable `fibcalc` dans le répertoire racine :

```bash
go build -o fibcalc ./cmd/fibcalc/main.go
./fibcalc -n 1000
```

### Lancer les tests

Pour exécuter la suite de tests (si des tests sont présents) :

```bash
go test -v ./...
```
