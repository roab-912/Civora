# Oracle ElectionGuard 2.0 de développement

Le SDK Kotlin est fixé au commit du verrou, et non à une branche mobile. `CivicVectors.kt` est un programme ajouté au checkout temporaire ; les primitives upstream ne sont pas modifiées. Le script compare les sources Kotlin, Gradle, TOML et le wrapper à l’archive exacte avant l’exécution. Les changements autorisés sont le harnais, sa tâche Gradle et la somme officielle de la distribution Gradle.

Sur Windows, après `npm ci --ignore-scripts` :

```powershell
.\scripts\electionguard-oracle.ps1 -Fetch
```

Le JDK 17, Gradle, le SDK et les caches restent dans `.tools/electionguard`. Les téléchargements du JDK, du SDK et de Gradle sont contrôlés par SHA-256. Sans `-Fetch`, les exécutions suivantes utilisent le cache hors ligne. Il faut Java 17 pour la compilation de référence. Les dépendances JVM sont celles du catalogue Gradle upstream, plus `slf4j-simple:2.0.9` pour exécuter le harnais ; la construction n’est pas présentée comme une chaîne de livraison hermétique qualifiée.

Le harnais crée cinq gardiens distincts, échange leurs parts chiffrées, vérifie huit bulletins et les deux totaux globaux, puis vérifie le refus avec seulement deux gardiens. Le modèle du manifeste est contrôlé par le validateur SDK. Les nonces de sélection viennent de `CiphertextBallot.Selection.selectionNonce`, avant sa suppression dans le bulletin public soumis.

Les secrets sont **publics, déterministes et exclusivement TEST_ONLY**. Le programme ne produit aucune clé de production. Il ne reconstruit pas la clé privée jointe. Les déchiffrements ne portent que sur le chiffré global de `yes_bit`.

En mode ordinaire, le programme retrouve les mêmes polynômes et bulletins, réutilise la preuve de total figée, la revérifie avec le SDK et compare tout le fichier au corpus suivi. Les preuves probabilistes ne sont pas remplacées pour masquer une divergence. Pour un nouveau tirage volontaire :

```powershell
.\scripts\electionguard-oracle.ps1 -NewProofs
```

L’ancien corpus est conservé sous un nom unique dans `.tools`. Ce mode exige ensuite de revoir les résultats et de régénérer les conversions, les empreintes et les fixtures. Il ne met pas automatiquement une suite en production.

Les étapes de développement sont `npm run build`, `node scripts/materialize-electionguard.mjs`, `node scripts/generate-electionguard-negatives.mjs`, puis la régénération explicite des preuves Semaphore si les octets liés ont changé. `node scripts/electionguard-lock.mjs --write` écrit le verrou après revue des changements. `node scripts/generate-encrypted-fixtures.mjs` construit les blocs ; des preuves existantes dont le contexte diffère sont refusées et demandent explicitement `--new-proofs`. Une nouvelle preuve modifie le corpus verrouillé : mettre à jour le verrou une fois les octets revus, puis `go run ./cmd/civcrypto fixtures` et `./scripts/check.ps1`. En utilisation ordinaire, préférer les modes `--check` et `-check`.

Le verrou distingue `development_interop_verified:true` de `public_pilot_qualified:false`. L’audit externe, les canaux auxiliaires et l’effacement mémoire du client JavaScript, l’indépendance opérationnelle des gardiens et la capacité publique restent à qualifier.