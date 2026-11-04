# Civora — vote secret de développement v0.4

Implémentation locale des jalons v0.1 à v0.4 de la [spécification](spec/Protocole-Civique-v1.md). Le noyau Go gère comptes, gouvernance, éligibilité Semaphore et vote chiffré ElectionGuard 2.0. Des vérificateurs publics Go et TypeScript contrôlent les cérémonies, les preuves 0–1 et le total global à seuil trois gardiens sur cinq.

Pour lancer la démonstration depuis PowerShell :

```powershell
.\scripts\demo.ps1
```

Elle exécute le scénario de gouvernance simulée de **99 blocs**, vérifie les vecteurs Semaphore **16 et 26**, puis rejoue un véritable scrutin ElectionGuard de **9 blocs**, exporte son registre public et le vérifie sans la base d’origine. Le scrutin chiffré compte trois Oui et deux Non et applique la loi dans le registre.

Le simulateur historique v0.2 reste compilé seulement avec `civora_sim`, dans `civsim`, et refusé par `civnode`. Les scénarios v0.4 utilisent le profil réel `electionguard_v0_4`, de vrais bulletins du SDK verrouillé et de vraies preuves Semaphore. Aucune PWA ni réseau de consensus n’est démarré.

Le blocage initial de sérialisation est résolu par une [annexe Civic explicite](spec/interop/electionguard-v0.4.md), conservant le design 2.0.0 et complétant les structures ouvertes de la référence 0.0.1. Le format compact, les sources, les artefacts et les vecteurs sont [verrouillés](spec/crypto/electionguard-v2.lock.json). Le SDK Kotlin épinglé a été compilé et exécuté ; son format n’est pas présenté comme le format officiel 0.0.1. La [décision 0010](spec/decisions/0010-electionguard-v0.4.md) détaille ce choix.

```sh
npm run fixtures:electionguard:check
npm run crypto:electionguard:review
npm run artifacts:electionguard:fetch
npm run crypto:electionguard:gate
go run ./cmd/civcrypto inspect -kind ballot -in spec/fixtures/v0.4/ballot.envelope.json
go run ./cmd/civnode replay -fixture spec/fixtures/v0.4/replay-mixed.json -store .dev-store/encrypted
go run ./cmd/civcrypto export -store .dev-store/encrypted -out .dev-store/public-encrypted.json
go run ./cmd/civcrypto audit -in .dev-store/public-encrypted.json
npm run crypto:electionguard:audit -- -in .dev-store/public-encrypted.json
```

La porte `crypto:electionguard:gate` contrôle le verrou et revérifie les chiffrés, les totaux et le refus de copie sous un autre nullifier. `qualified:false` signifie que le pilote public reste non qualifié ; `development_interop_verified:true` distingue le résultat de développement. L’inspecteur seul garde `cryptography_verified:false`. Les anciens fichiers `*.envelope.json` restent des marqueurs de transport sans contenu cryptographique valide ; les vraies fixtures sont `crypto.json` et `replay-*.json`.

Pour reconstruire l’oracle depuis l’archive du commit SDK exact, avec Java 17 et Gradle confinés à `.tools` : `./scripts/electionguard-oracle.ps1 -Fetch`. Les exécutions suivantes peuvent utiliser le cache hors ligne sans `-Fetch`. Les preuves de total figées sont revérifiées, sans changement de leurs octets. Le [manuel du harnais](tools/electionguard/README.md) précise la régénération explicite.

La preuve d’indépendance et le routage `scale_1` sont vérifiés sur une fixture Semaphore de profondeur 26, avec un contexte de point de contrôle fourni comme donnée de confiance. La réplication, l’authentification BFT de ce contexte, les journaux et les scellés de partitions restent à v0.5. Aucun bulletin n’est compté par ce vérificateur pur. **v0.4 est validé pour le développement ; le pilote public, l’audit externe et la capacité de masse restent à qualifier.**

Les empreintes des WASM, zkey et clés de vérification sont figées dans [le verrou Semaphore](spec/crypto/semaphore-v4.lock.json). Le SDK est fixé à **4.14.5**, avec ses circuits **4.13.0**. Les navigateurs contrôlent taille et SHA-256 avant chargement ; Go utilise les clés de vérification intégrées et contrôlées. Aucun votant ne choisit ses artefacts.

Prérequis : Go ≥ 1.26 et Node ≥ 24.12, npm. Les versions et dépendances utilisées sont consignées dans [le verrou de développement](spec/profiles/development.lock.json), `go.sum` et `package-lock.json`.

```sh
npm ci --ignore-scripts
npm test
go test ./... -count=1 -timeout 300s
go vet ./...
go test -tags civora_sim ./... -count=1 -timeout 300s
go vet -tags civora_sim ./...
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store .dev-store/activation -supported-epoch 2
go run -tags civora_sim ./cmd/civsim demo
go run -tags civora_sim ./cmd/civsim replay -fixture spec/fixtures/v0.2/governance.json -store .dev-store/governance
go run ./cmd/civnode replay -fixture spec/fixtures/v0.3/accounts.json -store .dev-store/eligibility-ledger
go run ./cmd/civeligibility index -ledger .dev-store/eligibility-ledger -store .dev-store/eligibility-index
go run ./cmd/civeligibility path -store .dev-store/eligibility-index -height 2 -index 1
go run ./cmd/civeligibility serve -store .dev-store/eligibility-index -addr 127.0.0.1:8783
```

Un rejeu répété sur le même répertoire reprend les blocs durables et vérifie leur identité. Sans `-supported-epoch 2`, le scénario d’activation s’arrête avant le bloc 3. Les CLI acceptent uniquement les genèses `mode:development`, `voting_profile:inline_dev`. Les anciens scénarios v0.1 conservent leurs racines.

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications, les deux modes de compilation, les contrôles de reproductibilité, les démonstrations, la projection de l’index et leur reprise. Les scripts utilisent Go installé dans `.tools/go` si disponible.

Pour les preuves dans un Web Worker sur Chromium et WebKit :

```sh
npm run artifacts:fetch
npm run browsers:install
npm run test:browser
```

Pour reconstruire l’arbre à **10 000 000 de feuilles actives**, exercer rotation/retrait et reprise, puis générer les preuves depuis le seul chemin HTTP dans les deux moteurs :

```sh
npm run test:scale
```

Cette vérification crée environ 640 Mo de niveaux d’arbre dans un nouveau répertoire de `.tools/qualification/`. Elle ne supprime pas les anciens essais. Les rapports restent dans `.tools/qualification/`. Les équivalents PowerShell sont `./scripts/check.ps1 -Browsers` et `./scripts/check.ps1 -Scale`.

Les tests navigateur utilisent une émulation mobile. La validation sur deux navigateurs de téléphones physiques reste à effectuer et n’est pas assimilée à cette émulation. Le rapport v0.3 distingue les critères exécutés et ce critère matériel restant.

L’API locale expose uniquement `GET /v1/eligibility/snapshots/latest`, `GET /v1/eligibility/snapshots/{height}` et `GET /v1/eligibility/snapshots/{height}/paths/{index}`. Elle ne fournit ni liste complète de membres ni route d’écriture. Le client doit comparer l’instantané et sa racine au contexte de registre de confiance ; HTTP et `snapshot_id` seuls ne prouvent pas la finalité.

Les tests couvrent gouvernance, quorums, conflits, éligibilité et altérations cryptographiques. **Trente-quatre scénarios distincts d’arrêt forcé** couvrent les essais antérieurs et six écritures de cérémonie, vote et dépouillement réels de part et d’autre de la frontière durable. La reprise retrouve les mêmes états, racines et chemins historiques. Les 59 vecteurs d’enveloppe et 38 refus cryptographiques sont partagés par Go et TypeScript.

Le TypeScript conserve l’oracle des comptes v0.1, vérifie les schémas et racines d’état, et utilise le SDK officiel comme oracle LeanIMT/Semaphore. Il ne réimplémente pas la machine de gouvernance ; l’ancien oracle refuse les genèses avec gouvernance ou arbre d’éligibilité.

Les [fixtures](spec/fixtures/) contiennent **des clés privées publiques de test**, explicitement étiquetées `TEST_ONLY`. `npm run fixtures` régénère v0.1 ; `go run -tags civora_sim ./cmd/civsim fixtures` régénère v0.2. Pour v0.3 : `npm run fixtures:eligibility`, puis `go run ./cmd/civeligibility fixtures`. Les identités et arbres sont déterministes ; les preuves Groth16 figées sont réutilisées et vérifiées. Leur régénération probabiliste exige explicitement `--new-proofs`. Les modes `--check` / `-check` ne réécrivent rien. `npm run schemas` régénère les schémas partagés.

Les décisions sont consignées dans [spec/decisions](spec/decisions/0010-electionguard-v0.4.md), avec formats de preuve, persistance et limites de qualification. Les ports `EligibilityProvider`, `MembershipVerifier`, `VotingCrypto` et `EffectAdapter` restent distincts. L’effet courant est exclusivement `ledger_only_v1`. Les archives n’incluent ni les fichiers `TEST_ONLY`, ni les nonces, ni les choix individuels. La genèse doit être authentifiée ; un export local n’est pas une preuve de consensus.

Les rotations de validateurs et gardiens, l’approbation et l’armement des mises à jour, CometBFT, la PWA, le déploiement et les outils d’urgence restent aux jalons correspondants. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l’arrêt de processus, pas une coupure électrique.
