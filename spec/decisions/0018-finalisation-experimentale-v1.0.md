# 0018 — Finalisation de l’édition expérimentale 1.0.0

Date : 2026-11-07. Complète la [décision 0017](0017-distribution-v1.0.md).

Le numéro de distribution passe de `1.0.0-rc.1` à **1.0.0**, canal `experimental`. Cette édition finalise la livraison locale. Le jalon **pilote public** de la spécification reste incomplet : la qualification v0.9 et les validations indépendantes manquent. Les champs `public_qualified`, `qualified_to_ten_million_ballots` et `milestone_complete` restent faux ; la capacité vérifiée reste `null`. Le numéro de la PWA, les époques, les domaines cryptographiques, les identifiants de schéma et les genèses sont conservés. `--public` demeure refusé.

## Sources reproductibles sans dépôt Git

L’archive source contient `.civora-source.json`, format `civora-source-1`, défini par `spec/distribution/source.schema.json`. Ce fichier énumère les chemins relatifs, tailles décimales et SHA-256 de chaque entrée source, dans l’ordre lexical ASCII. Il ne contient pas sa propre empreinte : l’empreinte de l’archive source engage ses octets. Toute confiance dans cet inventaire provient de la distribution authentifiée et de la provenance, jamais de son seul contenu.

Les propriétés inconnues, entrées absentes ou altérées, doublons même avec casse différente, chemins ambigus, sorties du répertoire, liens symboliques, entrées non triées et fichiers de plus de 64 Mio sont refusés. Les compilateurs utilisent une copie des seuls fichiers listés, vérifiée avant et après copie. Un fichier supplémentaire dans le répertoire extrait n’entre pas dans la construction. Si l’inventaire est présent mais invalide, la commande échoue sans revenir à Git.

Dans une copie de travail Git, la sélection précédente reste applicable. Dans une archive extraite, l’inventaire suffit : Git n’est pas requis. Les dépendances npm installées, le cache Go et les artefacts de preuve restent des entrées distinctes, épinglées par leurs verrous. `CIVORA_GO` et `CIVORA_ARTIFACT_DIR` permettent d’indiquer un Go installé et un cache d’artefacts existant ; les variables Go standard sont respectées. Ces chemins locaux ne sont pas inscrits dans les produits.

L’inventaire de dépendances passe à `civora-build-dependency-inventory-2` : `platforms` associe chaque cible aux modules Go du noyau et du vérificateur de distribution réellement utilisés pour cette cible. Les dépendances npm et les empreintes des verrous sont conservées. La provenance garde le format SLSA et l’assurance d’auto-attestation locale.

Les bundles Node sont minifiés pour éliminer les noms de chemin du répertoire de compilation. Le worker CommonJS conserve son fichier physique distinct. Les tests reconstruisent sous deux profondeurs de répertoire différentes et comparent tous les produits, puis exécutent les audits cryptographiques du paquet.

## Clôture locale vérifiable

`scripts/check.ps1 -Distribution -Report CHEMIN` produit un reçu local seulement après exécution des contrôles. Il lie résultat, options, outils, dates et empreinte des sources. Une modification des sources pendant les tests invalide ce reçu. L’empreinte exclut le rapport final généré `spec/reports/v1.0-distribution.json` pour éviter une dépendance circulaire ; l’archive source conserve néanmoins les octets de tous les fichiers sélectionnés, dont l’éventuel rapport précédent.

`release:finalize` exige les contrôles réseau, navigateur et distribution, les mêmes sources et outils, et une archive native identique à celle exécutée lors des tests. Il vérifie les empreintes, les sujets et la référence source de provenance puis contrôle le miroir TUF avec un état persistant. Les clés publiques de développement doivent être acceptées explicitement. Il écrit un rapport UTF-8 sans BOM, avec chemins portables pour les références d’artefacts.

Ce reçu local n’est pas une attestation d’un auditeur indépendant. Il ne permet ni publication publique ni activation. L’essai de reconstruction compare une copie Git et une copie des sources vérifiées avec Git indisponible, sur le même environnement. Il ne satisfait pas la condition de deux environnements indépendants. Les règles de confiance, seuils et expirations de 0017 restent applicables.
